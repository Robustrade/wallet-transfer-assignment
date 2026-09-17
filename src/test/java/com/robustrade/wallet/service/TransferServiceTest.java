package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.dto.CreateTransferRequest;
import com.robustrade.wallet.entities.LedgerEntry;
import com.robustrade.wallet.entities.Transfer;
import com.robustrade.wallet.entities.Wallet;
import com.robustrade.wallet.entities.Wallets;
import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.LedgerEntryType;
import com.robustrade.wallet.enums.TransferStatus;
import com.robustrade.wallet.enums.WalletStatus;
import com.robustrade.wallet.exception.IdempotencyConflictException;
import com.robustrade.wallet.exception.WalletNotFoundException;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.TransferRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

@ExtendWith(MockitoExtension.class)
class TransferServiceTest {

  private static final String KEY = "key-1";
  private static final UUID INITIATED_BY = UUID.randomUUID();

  @Mock private WalletRepository walletRepository;
  @Mock private TransferRepository transferRepository;
  @Mock private LedgerEntryRepository ledgerEntryRepository;

  @InjectMocks private TransferService service;

  @Test
  void movesMoneyWritesBothLedgerRowsAndMarksProcessed() {
    Wallet from = Wallets.active("wallet_1", "1000");
    Wallet to = Wallets.active("wallet_2", "500");
    Transfer stored = pendingTransfer("wallet_1", "wallet_2", "100");
    givenLocked(from, to);
    givenInsertSucceeds(stored);

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.replayed()).isFalse();
    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
    assertThat(from.getBalance()).isEqualByComparingTo("900");
    assertThat(to.getBalance()).isEqualByComparingTo("600");

    List<LedgerEntry> entries = savedLedgerEntries();
    assertThat(entries).hasSize(2);
    assertThat(entries.get(0).getType()).isEqualTo(LedgerEntryType.DEBIT);
    assertThat(entries.get(0).getWalletId()).isEqualTo("wallet_1");
    assertThat(entries.get(0).getCustomerId()).isEqualTo(from.getCustomerId());
    assertThat(entries.get(0).getAmount()).isEqualByComparingTo("100");
    assertThat(entries.get(1).getType()).isEqualTo(LedgerEntryType.CREDIT);
    assertThat(entries.get(1).getWalletId()).isEqualTo("wallet_2");
    assertThat(entries.get(1).getCustomerId()).isEqualTo(to.getCustomerId());
    assertThat(entries).allSatisfy(e -> assertThat(e.getTransferId()).isEqualTo(stored.getId()));
  }

  @Test
  void unknownSourceWalletIsRejected() {
    when(walletRepository.findByIdForUpdate("wallet_1")).thenReturn(Optional.empty());

    assertThatThrownBy(() -> transfer("wallet_1", "wallet_2", "100"))
        .isInstanceOf(WalletNotFoundException.class)
        .hasMessageContaining("wallet_1");

    verify(transferRepository, never()).insertIfAbsent(any(), any(), any(), any(), any(), any());
  }

  @Test
  void unknownDestinationWalletIsRejected() {
    when(walletRepository.findByIdForUpdate("wallet_1"))
        .thenReturn(Optional.of(Wallets.active("wallet_1", "1000")));
    when(walletRepository.findByIdForUpdate("wallet_2")).thenReturn(Optional.empty());

    assertThatThrownBy(() -> transfer("wallet_1", "wallet_2", "100"))
        .isInstanceOf(WalletNotFoundException.class)
        .hasMessageContaining("wallet_2");
  }

  @Test
  void reusedKeyWithAnIdenticalRequestReplaysWithoutTouchingWallets() {
    Transfer stored = pendingTransfer("wallet_1", "wallet_2", "100");
    stored.markProcessed();
    givenAlreadyApplied(stored);

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.replayed()).isTrue();
    assertThat(result.transfer()).isSameAs(stored);
    verify(walletRepository, never()).findByIdForUpdate(any());
    verify(transferRepository, never()).insertIfAbsent(any(), any(), any(), any(), any(), any());
    verify(ledgerEntryRepository, never()).save(any());
  }

  /**
   * A duplicate arriving while the first request is still open misses the pre-check, because the
   * other transaction's row is invisible until it commits. ON CONFLICT DO NOTHING blocks on the
   * unique index, and the read-back behind it is what makes the replay safe.
   */
  @Test
  void aConcurrentDuplicateReplaysThroughTheInsertPath() {
    Transfer stored = pendingTransfer("wallet_1", "wallet_2", "100");
    stored.markProcessed();
    givenLocked(Wallets.active("wallet_1", "1000"), Wallets.active("wallet_2", "500"));
    when(transferRepository.findByIdempotencyKey(KEY))
        .thenReturn(Optional.empty())
        .thenReturn(Optional.of(stored));
    when(transferRepository.insertIfAbsent(any(), any(), any(), any(), any(), any())).thenReturn(0);

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.replayed()).isTrue();
    verify(ledgerEntryRepository, never()).save(any());
  }

  /** The stored amount reads back with NUMERIC scale, which must not defeat the replay check. */
  @Test
  void replayToleratesScaleDifferencesInTheAmount() {
    givenAlreadyApplied(pendingTransfer("wallet_1", "wallet_2", "100.0000"));

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.replayed()).isTrue();
  }

  @Test
  void reusedKeyWithADifferentRequestIsAConflict() {
    givenAlreadyApplied(pendingTransfer("wallet_1", "wallet_2", "100"));

    assertThatThrownBy(() -> transfer("wallet_1", "wallet_2", "999"))
        .isInstanceOf(IdempotencyConflictException.class)
        .hasMessageContaining(KEY);

    verify(ledgerEntryRepository, never()).save(any());
  }

  @Test
  void inactiveSourceWalletFailsTheTransferInsteadOfThrowing() {
    Wallet from = Wallets.with("wallet_1", WalletStatus.BLOCKED, "1000");
    Wallet to = Wallets.active("wallet_2", "500");
    givenLocked(from, to);
    givenInsertSucceeds(pendingTransfer("wallet_1", "wallet_2", "100"));

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.FAILED);
    assertThat(result.transfer().getFailureReason()).isEqualTo(FailureReason.WALLET_NOT_ACTIVE);
    assertThat(result.replayed()).isFalse();
    assertThat(from.getBalance()).isEqualByComparingTo("1000");
    verify(ledgerEntryRepository, never()).save(any());
  }

  @Test
  void inactiveDestinationWalletFailsTheTransfer() {
    Wallet to = Wallets.with("wallet_2", WalletStatus.INACTIVE, "500");
    givenLocked(Wallets.active("wallet_1", "1000"), to);
    givenInsertSucceeds(pendingTransfer("wallet_1", "wallet_2", "100"));

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.transfer().getFailureReason()).isEqualTo(FailureReason.WALLET_NOT_ACTIVE);
    assertThat(to.getBalance()).isEqualByComparingTo("500");
  }

  @Test
  void insufficientFundsFailTheTransfer() {
    Wallet from = Wallets.active("wallet_1", "50");
    Wallet to = Wallets.active("wallet_2", "500");
    givenLocked(from, to);
    givenInsertSucceeds(pendingTransfer("wallet_1", "wallet_2", "100"));

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.FAILED);
    assertThat(result.transfer().getFailureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
    assertThat(from.getBalance()).isEqualByComparingTo("50");
    assertThat(to.getBalance()).isEqualByComparingTo("500");
    verify(ledgerEntryRepository, never()).save(any());
  }

  /** A balance exactly equal to the amount is spendable. */
  @Test
  void exactBalanceIsEnough() {
    givenLocked(Wallets.active("wallet_1", "100"), Wallets.active("wallet_2", "0"));
    givenInsertSucceeds(pendingTransfer("wallet_1", "wallet_2", "100"));

    TransferResult result = transfer("wallet_1", "wallet_2", "100");

    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
  }

  /** The native INSERT bypasses the persistence context, so a missing read-back is a real bug. */
  @Test
  void aTransferThatVanishesAfterInsertIsAnIllegalState() {
    givenLocked(Wallets.active("wallet_1", "1000"), Wallets.active("wallet_2", "500"));
    when(transferRepository.insertIfAbsent(any(), any(), any(), any(), any(), any())).thenReturn(1);
    when(transferRepository.findByIdempotencyKey(KEY)).thenReturn(Optional.empty());

    assertThatThrownBy(() -> transfer("wallet_1", "wallet_2", "100"))
        .isInstanceOf(IllegalStateException.class)
        .hasMessageContaining(KEY);
  }

  private TransferResult transfer(String fromId, String toId, String amount) {
    return service.createTransfer(request(fromId, toId, amount), INITIATED_BY);
  }

  private void givenLocked(Wallet from, Wallet to) {
    when(walletRepository.findByIdForUpdate(from.getId())).thenReturn(Optional.of(from));
    when(walletRepository.findByIdForUpdate(to.getId())).thenReturn(Optional.of(to));
  }

  private void givenInsertSucceeds(Transfer stored) {
    when(transferRepository.insertIfAbsent(any(), any(), any(), any(), any(), any())).thenReturn(1);
    when(transferRepository.findByIdempotencyKey(KEY))
        .thenReturn(Optional.empty()) // the pre-check: nothing stored under this key yet
        .thenReturn(Optional.of(stored)); // the read-back after the insert
  }

  private void givenAlreadyApplied(Transfer stored) {
    when(transferRepository.findByIdempotencyKey(KEY)).thenReturn(Optional.of(stored));
  }

  private List<LedgerEntry> savedLedgerEntries() {
    ArgumentCaptor<LedgerEntry> captor = ArgumentCaptor.forClass(LedgerEntry.class);
    verify(ledgerEntryRepository, org.mockito.Mockito.times(2)).save(captor.capture());
    return captor.getAllValues();
  }

  private static CreateTransferRequest request(String fromId, String toId, String amount) {
    return new CreateTransferRequest(KEY, fromId, toId, new BigDecimal(amount));
  }

  private static Transfer pendingTransfer(String fromId, String toId, String amount) {
    return new Transfer(KEY, fromId, toId, INITIATED_BY, new BigDecimal(amount));
  }
}
