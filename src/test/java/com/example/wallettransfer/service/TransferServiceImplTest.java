package com.example.wallettransfer.service;

import com.example.wallettransfer.domain.IdempotencyRecord;
import com.example.wallettransfer.domain.LedgerEntry;
import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.TransferStatus;
import com.example.wallettransfer.domain.Wallet;
import com.example.wallettransfer.domain.WalletFixtures;
import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;
import com.example.wallettransfer.exception.IdempotencyConflictException;
import com.example.wallettransfer.exception.InvalidTransferException;
import com.example.wallettransfer.exception.WalletNotFoundException;
import com.example.wallettransfer.repository.IdempotencyLockRepository;
import com.example.wallettransfer.repository.IdempotencyRecordRepository;
import com.example.wallettransfer.repository.LedgerEntryRepository;
import com.example.wallettransfer.repository.TransferRepository;
import com.example.wallettransfer.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import java.math.BigDecimal;
import java.util.List;
import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class TransferServiceImplTest {

    @Mock private WalletRepository walletRepository;
    @Mock private TransferRepository transferRepository;
    @Mock private LedgerEntryRepository ledgerEntryRepository;
    @Mock private IdempotencyRecordRepository idempotencyRecordRepository;
    @Mock private IdempotencyLockRepository idempotencyLockRepository;

    private TransferServiceImpl service;

    @BeforeEach
    void setUp() {
        service = new TransferServiceImpl(walletRepository, transferRepository, ledgerEntryRepository,
                idempotencyRecordRepository, idempotencyLockRepository);
    }

    @Test
    void processesTransferAndPersistsBalancedLedgerEntries() {
        CreateTransferRequest request = request("new-key", "source", "destination", "125.5000");
        Wallet source = wallet("source", "500.0000");
        Wallet destination = wallet("destination", "10.0000");
        setUpNewTransfer(request, source, destination);

        TransferResponse response = service.createTransfer(request);

        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(source.getBalance()).isEqualByComparingTo("374.5000");
        assertThat(destination.getBalance()).isEqualByComparingTo("135.5000");
        ArgumentCaptor<List<LedgerEntry>> entries = ArgumentCaptor.forClass(List.class);
        verify(ledgerEntryRepository).saveAll(entries.capture());
        assertThat(entries.getValue()).extracting(LedgerEntry::getType)
                .containsExactlyInAnyOrder(com.example.wallettransfer.domain.LedgerEntryType.DEBIT,
                        com.example.wallettransfer.domain.LedgerEntryType.CREDIT);
        assertThat(entries.getValue()).allSatisfy(entry -> assertThat(entry.getAmount()).isEqualByComparingTo("125.5000"));
    }

    @Test
    void replaysStoredTransferWithoutLockingWalletsOrWritingLedger() {
        CreateTransferRequest request = request("same-key", "source", "destination", "10");
        Transfer original = Transfer.pending("source", "destination", new BigDecimal("10.0000"));
        original.markProcessed();
        when(idempotencyRecordRepository.findWithTransferByKey("same-key"))
                .thenReturn(Optional.of(new IdempotencyRecord(
                        "same-key", com.example.wallettransfer.util.RequestHashGenerator.hash(request), original)));

        TransferResponse response = service.createTransfer(request);

        assertThat(response.transferId()).isEqualTo(original.getId());
        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        verify(walletRepository, never()).findByIdForUpdate(any());
        verify(ledgerEntryRepository, never()).saveAll(any());
    }

    @Test
    void rejectsReusedKeyWithDifferentBusinessPayload() {
        CreateTransferRequest request = request("same-key", "source", "destination", "10");
        Transfer original = Transfer.pending("source", "destination", new BigDecimal("10"));
        when(idempotencyRecordRepository.findWithTransferByKey("same-key"))
                .thenReturn(Optional.of(new IdempotencyRecord("same-key", "old-hash", original)));

        assertThatThrownBy(() -> service.createTransfer(request))
                .isInstanceOf(IdempotencyConflictException.class);

        verify(walletRepository, never()).findByIdForUpdate(any());
    }

    @Test
    void storesFailedTransferWhenSourceBalanceIsInsufficient() {
        CreateTransferRequest request = request("failed-key", "source", "destination", "75");
        Wallet source = wallet("source", "50");
        Wallet destination = wallet("destination", "10");
        setUpNewTransfer(request, source, destination);

        TransferResponse response = service.createTransfer(request);

        assertThat(response.status()).isEqualTo(TransferStatus.FAILED);
        assertThat(response.failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
        assertThat(source.getBalance()).isEqualByComparingTo("50");
        assertThat(destination.getBalance()).isEqualByComparingTo("10");
        verify(ledgerEntryRepository, never()).saveAll(any());
    }

    @Test
    void rejectsTransfersToTheSameWalletBeforeDatabaseWork() {
        CreateTransferRequest request = request("key", "wallet", "wallet", "1");

        assertThatThrownBy(() -> service.createTransfer(request))
                .isInstanceOf(InvalidTransferException.class)
                .hasMessageContaining("different");

        verify(idempotencyLockRepository, never()).lock(any());
    }

    @Test
    void reportsMissingWalletAfterAcquiringFirstWalletLock() {
        CreateTransferRequest request = request("key", "missing", "present", "1");
        when(idempotencyRecordRepository.findWithTransferByKey("key")).thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate("missing")).thenReturn(Optional.empty());

        assertThatThrownBy(() -> service.createTransfer(request))
                .isInstanceOf(WalletNotFoundException.class)
                .hasMessageContaining("missing");
    }

    @Test
    void processesTransferWhenSourceSortsAfterDestination() {
        CreateTransferRequest request = request("reverse-key", "z-source", "a-destination", "1");
        Wallet source = wallet("z-source", "10");
        Wallet destination = wallet("a-destination", "0");
        when(idempotencyRecordRepository.findWithTransferByKey("reverse-key")).thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate("a-destination")).thenReturn(Optional.of(destination));
        when(walletRepository.findByIdForUpdate("z-source")).thenReturn(Optional.of(source));
        when(transferRepository.saveAndFlush(any(Transfer.class))).thenAnswer(invocation -> invocation.getArgument(0));

        TransferResponse response = service.createTransfer(request);

        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(source.getBalance()).isEqualByComparingTo("9");
        assertThat(destination.getBalance()).isEqualByComparingTo("1");
    }

    private void setUpNewTransfer(CreateTransferRequest request, Wallet source, Wallet destination) {
        when(idempotencyRecordRepository.findWithTransferByKey(request.idempotencyKey())).thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate("destination")).thenReturn(Optional.of(destination));
        when(walletRepository.findByIdForUpdate("source")).thenReturn(Optional.of(source));
        when(transferRepository.saveAndFlush(any(Transfer.class))).thenAnswer(invocation -> invocation.getArgument(0));
    }

    private CreateTransferRequest request(String key, String source, String destination, String amount) {
        return new CreateTransferRequest(key, source, destination, new BigDecimal(amount));
    }

    private Wallet wallet(String id, String balance) {
        return WalletFixtures.wallet(id, balance);
    }
}
