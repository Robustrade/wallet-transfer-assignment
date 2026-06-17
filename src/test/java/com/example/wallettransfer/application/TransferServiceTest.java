package com.example.wallettransfer.application;

import com.example.wallettransfer.domain.TransferState;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;

import java.time.OffsetDateTime;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

@SpringBootTest
@ActiveProfiles("test")
class TransferServiceTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private JdbcTemplate jdbcTemplate;

    @BeforeEach
    void setUp() {
        jdbcTemplate.update("delete from ledger_entries");
        jdbcTemplate.update("delete from transfers");
        jdbcTemplate.update("delete from wallets");
        insertWallet("wallet_1", 500);
        insertWallet("wallet_2", 100);
    }

    @Test
    void successfulTransferUpdatesBalancesAndWritesBalancedLedgerEntries() {
        TransferResult result = transferService.createTransfer(
                new CreateTransferCommand("key-success", "wallet_1", "wallet_2", 125));

        assertEquals(TransferState.PROCESSED, result.state());
        assertEquals(375, walletBalance("wallet_1"));
        assertEquals(225, walletBalance("wallet_2"));
        assertEquals(2, countRows("ledger_entries"));
        assertEquals(125, ledgerAmount("wallet_1", "DEBIT"));
        assertEquals(125, ledgerAmount("wallet_2", "CREDIT"));
    }

    @Test
    void duplicateIdempotencyKeyReturnsOriginalResultWithoutRepeatingSideEffects() {
        CreateTransferCommand command = new CreateTransferCommand("key-retry", "wallet_1", "wallet_2", 100);

        TransferResult first = transferService.createTransfer(command);
        TransferResult second = transferService.createTransfer(command);

        assertEquals(first.transferId(), second.transferId());
        assertEquals(TransferState.PROCESSED, second.state());
        assertEquals(400, walletBalance("wallet_1"));
        assertEquals(200, walletBalance("wallet_2"));
        assertEquals(2, countRows("ledger_entries"));
    }

    @Test
    void sameIdempotencyKeyWithDifferentPayloadIsRejected() {
        TransferResult first = transferService.createTransfer(
                new CreateTransferCommand("key-mismatch", "wallet_1", "wallet_2", 100));

        IdempotencyKeyReuseException exception = assertThrows(
                IdempotencyKeyReuseException.class,
                () -> transferService.createTransfer(
                        new CreateTransferCommand("key-mismatch", "wallet_1", "wallet_2", 101)));

        assertEquals("Idempotency key was already used for a different request", exception.getMessage());
        assertEquals(400, walletBalance("wallet_1"));
        assertEquals(200, walletBalance("wallet_2"));
        assertEquals(2, countRows("ledger_entries"));
        assertNotEquals(first.transferId(), null);
    }

    @Test
    void insufficientFundsMarksTransferFailedWithoutLedgerEntries() {
        CreateTransferCommand command = new CreateTransferCommand("key-insufficient", "wallet_1", "wallet_2", 900);

        TransferResult first = transferService.createTransfer(command);
        TransferResult retry = transferService.createTransfer(command);

        assertEquals(first.transferId(), retry.transferId());
        assertEquals(TransferState.FAILED, first.state());
        assertEquals("INSUFFICIENT_FUNDS", first.failureReason());
        assertEquals(500, walletBalance("wallet_1"));
        assertEquals(100, walletBalance("wallet_2"));
        assertEquals(0, countRows("ledger_entries"));
    }

    @Test
    void invalidTransferRequestIsRejectedBeforePersistence() {
        assertThrows(
                InvalidTransferRequestException.class,
                () -> transferService.createTransfer(
                        new CreateTransferCommand(" ", "wallet_1", "wallet_2", 100)));

        assertThrows(
                InvalidTransferRequestException.class,
                () -> transferService.createTransfer(
                        new CreateTransferCommand("key-same-wallet", "wallet_1", "wallet_1", 100)));

        assertThrows(
                InvalidTransferRequestException.class,
                () -> transferService.createTransfer(
                        new CreateTransferCommand("key-negative", "wallet_1", "wallet_2", -1)));

        assertEquals(0, countRows("transfers"));
        assertEquals(0, countRows("ledger_entries"));
    }

    @Test
    void missingWalletIsRejectedWithoutLedgerEntries() {
        WalletNotFoundException exception = assertThrows(
                WalletNotFoundException.class,
                () -> transferService.createTransfer(
                        new CreateTransferCommand("key-missing-wallet", "wallet_1", "wallet_missing", 100)));

        assertEquals("One or both wallets do not exist", exception.getMessage());
        assertEquals(0, countRows("transfers"));
        assertEquals(0, countRows("ledger_entries"));
    }

    private void insertWallet(String id, long balance) {
        OffsetDateTime now = OffsetDateTime.now();
        jdbcTemplate.update(
                "insert into wallets (id, balance, created_at, updated_at) values (?, ?, ?, ?)",
                id,
                balance,
                now,
                now);
    }

    private long walletBalance(String walletId) {
        return jdbcTemplate.queryForObject(
                "select balance from wallets where id = ?",
                Long.class,
                walletId);
    }

    private int countRows(String tableName) {
        return jdbcTemplate.queryForObject("select count(*) from " + tableName, Integer.class);
    }

    private long ledgerAmount(String walletId, String entryType) {
        return jdbcTemplate.queryForObject(
                "select amount from ledger_entries where wallet_id = ? and entry_type = ?",
                Long.class,
                walletId,
                entryType);
    }
}
