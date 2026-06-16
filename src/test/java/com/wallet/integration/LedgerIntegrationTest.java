package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.controller.dto.TransferResponse;
import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import java.util.List;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for double-entry ledger correctness.
 * Verifies that every transfer produces exactly two balanced ledger entries.
 */
class LedgerIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Test
    @DisplayName("Should create exactly two ledger entries per transfer")
    void shouldCreateTwoLedgerEntriesPerTransfer() {
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 300L);

        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate,
                "ledger-two-" + UUID.randomUUID(),
                request,
                TransferResponse.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        UUID transferId = response.getBody().id();

        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transferId);

        assertThat(entries).hasSize(2);
    }

    @Test
    @DisplayName("Should create matching DEBIT and CREDIT entries")
    void shouldCreateMatchingDebitAndCredit() {
        long amount = 750L;
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", amount);

        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate,
                "ledger-match-" + UUID.randomUUID(),
                request,
                TransferResponse.class
        );

        UUID transferId = response.getBody().id();
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transferId);

        // Find debit and credit entries
        LedgerEntry debit = entries.stream()
                .filter(e -> e.getEntryType() == EntryType.DEBIT)
                .findFirst().orElseThrow();
        LedgerEntry credit = entries.stream()
                .filter(e -> e.getEntryType() == EntryType.CREDIT)
                .findFirst().orElseThrow();

        // Debit should be from source wallet
        assertThat(debit.getWalletId()).isEqualTo("wallet_1");
        assertThat(debit.getAmount()).isEqualTo(amount);

        // Credit should be to destination wallet
        assertThat(credit.getWalletId()).isEqualTo("wallet_2");
        assertThat(credit.getAmount()).isEqualTo(amount);
    }

    @Test
    @DisplayName("Should maintain a balanced ledger (total debits == total credits)")
    void shouldMaintainBalancedLedger() {
        // Execute a few transfers
        for (int i = 0; i < 5; i++) {
            CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 10L);
            postTransfer(
                    restTemplate,
                    "ledger-balance-" + i + "-" + UUID.randomUUID(),
                    request,
                    TransferResponse.class
            );
        }

        // The net ledger balance should be 0 (total credits - total debits)
        Long netBalance = ledgerEntryRepository.computeNetLedgerBalance();
        assertThat(netBalance).isEqualTo(0L);
    }

    @Test
    @DisplayName("Should not create ledger entries for failed transfers")
    void shouldNotCreateLedgerEntriesForFailedTransfer() {
        // wallet_4 has 0 balance — transfer will fail with insufficient balance.
        // The failure recorder still persists a FAILED Transfer row for audit.
        CreateTransferRequest request = new CreateTransferRequest("wallet_4", "wallet_1", 999L);

        ResponseEntity<Object> response = postTransfer(
                restTemplate,
                "ledger-nofail-" + UUID.randomUUID(),
                request,
                Object.class
        );
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);

        // Locate the FAILED Transfer row created by the failure recorder, then
        // verify its specific id has zero ledger entries — a stronger assertion
        // than searching by walletId for an amount.
        List<Transfer> failedTransfers = transferRepository.findByWalletId("wallet_4", 100).stream()
                .filter(t -> t.getStatus() == TransferStatus.FAILED && t.getAmount() == 999L)
                .toList();
        assertThat(failedTransfers).hasSize(1);
        UUID failedTransferId = failedTransfers.get(0).getId();

        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(failedTransferId);
        assertThat(entries).isEmpty();
    }

    @Test
    @DisplayName("Should not create duplicate ledger entries on idempotent replay")
    void shouldNotDuplicateLedgerOnReplay() {
        String idempotencyKey = "ledger-idem-" + UUID.randomUUID();
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 25L);

        // First request
        ResponseEntity<TransferResponse> first = postTransfer(
                restTemplate, idempotencyKey, request, TransferResponse.class);
        UUID transferId = first.getBody().id();

        // Replay
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);

        // Should still have exactly 2 entries for this transfer
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transferId);
        assertThat(entries).hasSize(2);
    }

    @Autowired
    private org.jooq.DSLContext dsl;

    @Test
    @DisplayName("Should track running balances correctly for both wallets")
    void shouldTrackRunningBalancesCorrectly() {
        // Get initial balances
        var balance1 = getWithClient(restTemplate, "/wallets/wallet_1/balance", java.util.Map.class).getBody();
        long initialBalance1 = ((Number) balance1.get("balance")).longValue();

        var balance2 = getWithClient(restTemplate, "/wallets/wallet_2/balance", java.util.Map.class).getBody();
        long initialBalance2 = ((Number) balance2.get("balance")).longValue();

        long amount = 150L;
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", amount);
        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate,
                "ledger-running-" + UUID.randomUUID(),
                request,
                TransferResponse.class
        );

        UUID transferId = response.getBody().id();
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transferId);

        LedgerEntry debit = entries.stream()
                .filter(e -> e.getEntryType() == EntryType.DEBIT)
                .findFirst().orElseThrow();
        LedgerEntry credit = entries.stream()
                .filter(e -> e.getEntryType() == EntryType.CREDIT)
                .findFirst().orElseThrow();

        // Check running balances match wallet balances after transfer
        assertThat(debit.getRunningBalance()).isEqualTo(initialBalance1 - amount);
        assertThat(credit.getRunningBalance()).isEqualTo(initialBalance2 + amount);
    }

    @Test
    @DisplayName("Should enforce ledger entries immutability at database level")
    void shouldEnforceLedgerImmutability() {
        // Create a transfer to generate ledger entries
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate,
                "ledger-immutable-" + UUID.randomUUID(),
                request,
                TransferResponse.class
        );
        UUID transferId = response.getBody().id();
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transferId);
        assertThat(entries).isNotEmpty();
        UUID entryId = entries.get(0).getId();

        // 1. Try to UPDATE the ledger entry amount
        org.assertj.core.api.Assertions.assertThatThrownBy(() -> {
            dsl.execute("UPDATE ledger_entries SET amount = 999999 WHERE id = ?", entryId);
        }).isInstanceOf(org.jooq.exception.DataAccessException.class);

        // 2. Try to DELETE the ledger entry
        org.assertj.core.api.Assertions.assertThatThrownBy(() -> {
            dsl.execute("DELETE FROM ledger_entries WHERE id = ?", entryId);
        }).isInstanceOf(org.jooq.exception.DataAccessException.class);
    }
}
