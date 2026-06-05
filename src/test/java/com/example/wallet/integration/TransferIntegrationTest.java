package com.example.wallet.integration;

import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.ErrorResponse;
import com.example.wallet.handler.dto.TransferResponse;
import com.example.wallet.model.TransferStatus;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;

import java.math.BigDecimal;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for POST /transfers and GET /wallets/{id}/balance.
 *
 * Runs against a real PostgreSQL instance via Testcontainers (tc: JDBC URL in
 * application-test.yml). Flyway migrations — including V4 seed data — are applied
 * on first container start.
 *
 * Each test resets wallet balances and clears transfer tables in @BeforeEach
 * to guarantee test isolation.
 *
 * Covers:
 *   - happy path: balances, HTTP 201, ledger entries
 *   - idempotency: duplicate key → 200, same id, balances unchanged
 *   - insufficient funds: 422, no ledger, balance unchanged
 *   - validation: 400 for missing fields, negative amount, self-transfer
 *   - wallet not found: 404
 *   - ledger balance invariant: SUM(DEBIT) == SUM(CREDIT) == amount
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles("test")
class TransferIntegrationTest {

    @Autowired private TestRestTemplate restTemplate;
    @Autowired private JdbcTemplate     jdbc;

    // ── Test lifecycle ────────────────────────────────────────────────────────

    @BeforeEach
    void resetState() {
        // Clear transactional tables — order matters (FK constraints)
        jdbc.execute("DELETE FROM idempotency_records");
        jdbc.execute("DELETE FROM ledger_entries");
        jdbc.execute("DELETE FROM transfers");

        // Reset seed wallet balances to known values
        jdbc.execute("UPDATE wallets SET balance = 1000.0000 WHERE id = 'wallet_alice'");
        jdbc.execute("UPDATE wallets SET balance =  500.0000 WHERE id = 'wallet_bob'");
        jdbc.execute("UPDATE wallets SET balance =    0.0000 WHERE id = 'wallet_carol'");
    }

    // ── Happy path ────────────────────────────────────────────────────────────

    @Test
    @DisplayName("POST /transfers — 201, source debited, destination credited")
    void transfer_success_updatesBalancesCorrectly() {
        ResponseEntity<TransferResponse> resp = postTransfer(
                new CreateTransferRequest("key-1", "wallet_alice", "wallet_bob", new BigDecimal("200")));

        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(resp.getBody()).isNotNull();
        assertThat(resp.getBody().status()).isEqualTo(TransferStatus.PROCESSED);

        assertBalance("wallet_alice",  800);
        assertBalance("wallet_bob",    700);
    }

    @Test
    @DisplayName("POST /transfers — exactly 2 ledger entries (1 DEBIT + 1 CREDIT)")
    void transfer_success_writesTwoLedgerEntries() {
        ResponseEntity<TransferResponse> resp = postTransfer(
                new CreateTransferRequest("key-2", "wallet_alice", "wallet_bob", new BigDecimal("100")));

        UUID transferId = resp.getBody().id();
        int count = countLedger(transferId);
        assertThat(count).isEqualTo(2);

        // Verify one DEBIT and one CREDIT
        int debits  = jdbc.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ? AND type = 'DEBIT'",
                Integer.class, transferId);
        int credits = jdbc.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ? AND type = 'CREDIT'",
                Integer.class, transferId);
        assertThat(debits).isEqualTo(1);
        assertThat(credits).isEqualTo(1);
    }

    @Test
    @DisplayName("POST /transfers — ledger DEBIT and CREDIT each equal the transfer amount")
    void transfer_success_ledgerEntriesMatchAmount() {
        BigDecimal amount = new BigDecimal("350.00");
        ResponseEntity<TransferResponse> resp = postTransfer(
                new CreateTransferRequest("key-3", "wallet_alice", "wallet_bob", amount));

        UUID transferId = resp.getBody().id();

        BigDecimal debitAmount = jdbc.queryForObject(
                "SELECT amount FROM ledger_entries WHERE transfer_id = ? AND type = 'DEBIT'",
                BigDecimal.class, transferId);
        BigDecimal creditAmount = jdbc.queryForObject(
                "SELECT amount FROM ledger_entries WHERE transfer_id = ? AND type = 'CREDIT'",
                BigDecimal.class, transferId);

        assertThat(debitAmount).isEqualByComparingTo(amount);
        assertThat(creditAmount).isEqualByComparingTo(amount);
    }

    @Test
    @DisplayName("GET /wallets/{id}/balance — returns correct balance after transfer")
    void getBalance_returnsUpdatedBalance() {
        postTransfer(new CreateTransferRequest("key-4", "wallet_alice", "wallet_carol", new BigDecimal("150")));

        ResponseEntity<String> resp = restTemplate.getForEntity(
                "/wallets/wallet_alice/balance", String.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(resp.getBody()).contains("850");
    }

    // ── Idempotency ───────────────────────────────────────────────────────────

    @Test
    @DisplayName("duplicate idempotency key — second request returns 200 with same transfer id")
    void transfer_duplicateKey_returns200WithSameId() {
        CreateTransferRequest req =
                new CreateTransferRequest("key-idem-1", "wallet_alice", "wallet_bob", new BigDecimal("100"));

        ResponseEntity<TransferResponse> first  = postTransfer(req);
        ResponseEntity<TransferResponse> second = postTransfer(req);

        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(second.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(second.getBody().id()).isEqualTo(first.getBody().id());
        assertThat(second.getBody().status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    @DisplayName("duplicate idempotency key — wallet balance changes only once")
    void transfer_duplicateKey_balanceChangedOnce() {
        CreateTransferRequest req =
                new CreateTransferRequest("key-idem-2", "wallet_alice", "wallet_bob", new BigDecimal("100"));

        postTransfer(req);
        postTransfer(req); // replay

        assertBalance("wallet_alice", 900); // deducted exactly once
        assertBalance("wallet_bob",   600); // credited exactly once
    }

    @Test
    @DisplayName("duplicate idempotency key — exactly 2 ledger entries total (not 4)")
    void transfer_duplicateKey_ledgerEntriesNotDuplicated() {
        CreateTransferRequest req =
                new CreateTransferRequest("key-idem-3", "wallet_alice", "wallet_bob", new BigDecimal("100"));

        ResponseEntity<TransferResponse> first = postTransfer(req);
        postTransfer(req); // replay

        assertThat(countLedger(first.getBody().id())).isEqualTo(2);
    }

    // ── Insufficient funds ────────────────────────────────────────────────────

    @Test
    @DisplayName("insufficient funds — 422, balance unchanged, no ledger entries")
    void transfer_insufficientFunds_returns422AndNoSideEffects() {
        // wallet_carol has 0 balance
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest("key-nsf-1", "wallet_carol", "wallet_bob", new BigDecimal("100")),
                ErrorResponse.class);

        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(resp.getBody().error()).isEqualTo("insufficient_funds");

        assertBalance("wallet_carol", 0);
        assertBalance("wallet_bob",   500);

        // Transfer was stored as FAILED but no ledger entries
        int ledgerCount = jdbc.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries", Integer.class);
        assertThat(ledgerCount).isZero();
    }

    @Test
    @DisplayName("insufficient funds idempotent replay — still returns 422")
    void transfer_insufficientFunds_replayAlsoReturns422() {
        CreateTransferRequest req =
                new CreateTransferRequest("key-nsf-2", "wallet_carol", "wallet_bob", new BigDecimal("100"));

        ResponseEntity<ErrorResponse> first  = restTemplate.postForEntity("/transfers", req, ErrorResponse.class);
        ResponseEntity<ErrorResponse> second = restTemplate.postForEntity("/transfers", req, ErrorResponse.class);

        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(second.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(second.getBody().error()).isEqualTo("insufficient_funds");
    }

    // ── Validation errors ─────────────────────────────────────────────────────

    @Test
    @DisplayName("missing idempotency key — 400")
    void transfer_missingIdempotencyKey_returns400() {
        // Use a raw map to bypass record constructor — send null idempotencyKey
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest(null, "wallet_alice", "wallet_bob", new BigDecimal("10")),
                ErrorResponse.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("negative amount — 400")
    void transfer_negativeAmount_returns400() {
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest("key-neg", "wallet_alice", "wallet_bob", new BigDecimal("-50")),
                ErrorResponse.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("zero amount — 400")
    void transfer_zeroAmount_returns400() {
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest("key-zero", "wallet_alice", "wallet_bob", BigDecimal.ZERO),
                ErrorResponse.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("self-transfer — 400")
    void transfer_selfTransfer_returns400() {
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest("key-self", "wallet_alice", "wallet_alice", new BigDecimal("100")),
                ErrorResponse.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(resp.getBody().error()).contains("must not be the same");
    }

    @Test
    @DisplayName("non-existent wallet — 404")
    void transfer_walletNotFound_returns404() {
        ResponseEntity<ErrorResponse> resp = restTemplate.postForEntity(
                "/transfers",
                new CreateTransferRequest("key-404", "wallet_ghost", "wallet_bob", new BigDecimal("10")),
                ErrorResponse.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
        assertThat(resp.getBody().error()).isEqualTo("wallet_not_found");
    }

    @Test
    @DisplayName("GET /wallets/{id}/balance — 404 for unknown wallet")
    void getBalance_unknownWallet_returns404() {
        ResponseEntity<String> resp = restTemplate.getForEntity(
                "/wallets/wallet_ghost/balance", String.class);
        assertThat(resp.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }

    // ── Helpers ───────────────────────────────────────────────────────────────

    private ResponseEntity<TransferResponse> postTransfer(CreateTransferRequest req) {
        return restTemplate.postForEntity("/transfers", req, TransferResponse.class);
    }

    private void assertBalance(String walletId, long expected) {
        BigDecimal balance = jdbc.queryForObject(
                "SELECT balance FROM wallets WHERE id = ?", BigDecimal.class, walletId);
        assertThat(balance).isEqualByComparingTo(BigDecimal.valueOf(expected));
    }

    private int countLedger(UUID transferId) {
        return jdbc.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?",
                Integer.class, transferId);
    }
}
