package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.TransactionType;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.StatementEntryResponse;
import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

@ExtendWith(MockitoExtension.class)
class StatementServiceImplTest {

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @InjectMocks
    private StatementServiceImpl statementService;

    @Test
    void getStatement_byWalletId_returnsChronologicalEntriesWithRunningBalances() {
        Wallet wallet = wallet("wallet-1", 1001L, "900.00");
        UUID transfer1 = UUID.randomUUID();
        UUID transfer2 = UUID.randomUUID();
        Instant t1 = Instant.parse("2026-09-13T10:00:00Z");
        Instant t2 = Instant.parse("2026-09-13T11:00:00Z");

        List<LedgerEntry> ledgerEntries = List.of(
                ledgerEntry(wallet.getId(), transfer1, TransactionType.CREDIT, "1000.00", t1),
                ledgerEntry(wallet.getId(), transfer2, TransactionType.DEBIT, "100.00", t2)
        );

        when(walletRepository.findByIdForUpdate("wallet-1")).thenReturn(Optional.of(wallet));
        when(ledgerEntryRepository.findByWalletIdOrderByCreatedAtAscIdAsc("wallet-1"))
                .thenReturn(ledgerEntries);

        StatementResponse response = statementService.getStatement(null, "wallet-1");

        assertThat(response.getWalletId()).isEqualTo("wallet-1");
        assertThat(response.getUserId()).isEqualTo(1001L);
        assertThat(response.getCurrentBalance()).isEqualByComparingTo("900.00");
        assertThat(response.getEntries()).hasSize(2);

        StatementEntryResponse first = response.getEntries().get(0);
        assertThat(first.getTransferId()).isEqualTo(transfer1);
        assertThat(first.getTransferType()).isEqualTo(TransactionType.CREDIT);
        assertThat(first.getAmount()).isEqualByComparingTo("1000.00");
        assertThat(first.getPreviousBalance()).isEqualByComparingTo("0.00");
        assertThat(first.getBalanceAfterTransfer()).isEqualByComparingTo("1000.00");
        assertThat(first.getTransferDate()).isEqualTo(t1);

        StatementEntryResponse second = response.getEntries().get(1);
        assertThat(second.getTransferId()).isEqualTo(transfer2);
        assertThat(second.getTransferType()).isEqualTo(TransactionType.DEBIT);
        assertThat(second.getAmount()).isEqualByComparingTo("100.00");
        assertThat(second.getPreviousBalance()).isEqualByComparingTo("1000.00");
        assertThat(second.getBalanceAfterTransfer()).isEqualByComparingTo("900.00");
        assertThat(second.getTransferDate()).isEqualTo(t2);

        verify(walletRepository).findByIdForUpdate("wallet-1");
        verify(ledgerEntryRepository).findByWalletIdOrderByCreatedAtAscIdAsc("wallet-1");
    }

    @Test
    void getStatement_byUserId_whenSingleWallet_returnsStatement() {
        Wallet wallet = wallet("wallet-1", 1001L, "500.00");

        when(walletRepository.findByUserId(1001L)).thenReturn(List.of(wallet));
        when(walletRepository.findByIdForUpdate("wallet-1")).thenReturn(Optional.of(wallet));
        when(ledgerEntryRepository.findByWalletIdOrderByCreatedAtAscIdAsc("wallet-1"))
                .thenReturn(List.of());

        StatementResponse response = statementService.getStatement(1001L, null);

        assertThat(response.getWalletId()).isEqualTo("wallet-1");
        assertThat(response.getUserId()).isEqualTo(1001L);
        assertThat(response.getCurrentBalance()).isEqualByComparingTo("500.00");
        assertThat(response.getEntries()).isEmpty();
        verify(walletRepository).findByUserId(1001L);
        verify(walletRepository).findByIdForUpdate("wallet-1");
    }

    @Test
    void getStatement_whenBothParamsProvidedAndMatch_usesWalletId() {
        Wallet wallet = wallet("wallet-1", 1001L, "100.00");

        when(walletRepository.findByIdForUpdate("wallet-1")).thenReturn(Optional.of(wallet));
        when(ledgerEntryRepository.findByWalletIdOrderByCreatedAtAscIdAsc("wallet-1"))
                .thenReturn(List.of());

        StatementResponse response = statementService.getStatement(1001L, "wallet-1");

        assertThat(response.getWalletId()).isEqualTo("wallet-1");
        assertThat(response.getUserId()).isEqualTo(1001L);
        verify(walletRepository).findByIdForUpdate("wallet-1");
    }

    @Test
    void getStatement_whenNeitherParamProvided_throwsValidationError() {
        assertThatThrownBy(() -> statementService.getStatement(null, null))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("VALIDATION_ERROR");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(400);
                });
    }

    @Test
    void getStatement_whenWalletNotFound_throwsNotFound() {
        when(walletRepository.findByIdForUpdate("missing-wallet")).thenReturn(Optional.empty());

        assertThatThrownBy(() -> statementService.getStatement(null, "missing-wallet"))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("WALLET_NOT_FOUND");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(404);
                });
    }

    @Test
    void getStatement_whenUserHasMultipleWallets_throwsBadRequest() {
        when(walletRepository.findByUserId(1001L)).thenReturn(List.of(
                wallet("wallet-1", 1001L, "100.00"),
                wallet("wallet-2", 1001L, "200.00")
        ));

        assertThatThrownBy(() -> statementService.getStatement(1001L, null))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("MULTIPLE_WALLETS");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(400);
                });
    }

    @Test
    void getStatement_whenWalletDoesNotBelongToUser_throwsMismatch() {
        Wallet wallet = wallet("wallet-1", 2002L, "100.00");
        when(walletRepository.findByIdForUpdate("wallet-1")).thenReturn(Optional.of(wallet));

        assertThatThrownBy(() -> statementService.getStatement(1001L, "wallet-1"))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("WALLET_USER_MISMATCH");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(400);
                });
    }

    private static Wallet wallet(String id, Long userId, String balance) {
        Instant now = Instant.parse("2026-09-13T09:00:00Z");
        return Wallet.builder()
                .id(id)
                .userId(userId)
                .balance(new BigDecimal(balance))
                .createdAt(now)
                .updatedAt(now)
                .build();
    }

    private static LedgerEntry ledgerEntry(
            String walletId,
            UUID transferId,
            TransactionType type,
            String amount,
            Instant createdAt
    ) {
        return LedgerEntry.builder()
                .id(UUID.randomUUID())
                .walletId(walletId)
                .transferId(transferId)
                .transactionType(type)
                .amount(new BigDecimal(amount))
                .createdAt(createdAt)
                .build();
    }
}
