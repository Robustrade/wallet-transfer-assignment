package com.robustrade.wallet;

import static org.assertj.core.api.Assertions.assertThatCode;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.robustrade.wallet.support.IntegrationTest;
import java.util.UUID;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.dao.DataIntegrityViolationException;

/**
 * The schema is the last line of defence: every rule here is also enforced in Java, and these tests
 * prove the database still rejects the row if that Java check is ever bypassed or removed.
 */
@Tag("integration")
class DatabaseConstraintsIntegrationTest extends IntegrationTest {

  private static final UUID CUSTOMER = UUID.fromString("11111111-1111-1111-1111-111111111111");

  @Test
  void anIdempotencyKeyCannotBeReused() {
    insertTransfer(UUID.randomUUID(), "key", "wallet_1", "wallet_2", "100", "PENDING", null);

    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(), "key", "wallet_1", "wallet_2", "100", "PENDING", null))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  @Test
  void aTransferAmountMustBePositive() {
    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(), "zero", "wallet_1", "wallet_2", "0", "PENDING", null))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  @Test
  void aTransferCannotTargetItsOwnWallet() {
    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(), "self", "wallet_1", "wallet_1", "100", "PENDING", null))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  @Test
  void aTransferMustReferenceWalletsThatExist() {
    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(), "ghost", "wallet_1", "wallet_9", "100", "PENDING", null))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  /**
   * transfers_reason_iff_failed: a reason without FAILED, or FAILED without a reason, is invalid.
   */
  @Test
  void aFailureReasonIsRequiredByAndOnlyByTheFailedStatus() {
    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(), "a", "wallet_1", "wallet_2", "100", "FAILED", null))
        .isInstanceOf(DataIntegrityViolationException.class);

    assertThatThrownBy(
            () ->
                insertTransfer(
                    UUID.randomUUID(),
                    "b",
                    "wallet_1",
                    "wallet_2",
                    "100",
                    "PROCESSED",
                    "INSUFFICIENT_FUNDS"))
        .isInstanceOf(DataIntegrityViolationException.class);

    assertThatCode(
            () ->
                insertTransfer(
                    UUID.randomUUID(),
                    "c",
                    "wallet_1",
                    "wallet_2",
                    "100",
                    "FAILED",
                    "INSUFFICIENT_FUNDS"))
        .doesNotThrowAnyException();
  }

  /**
   * ledger_entries_one_per_type stops a transfer being debited twice. Note it cannot require that
   * both rows exist: that guarantee comes from the transaction, not the schema.
   */
  @Test
  void aTransferCannotHaveTwoEntriesOfTheSameType() {
    UUID transferId = UUID.randomUUID();
    insertTransfer(transferId, "ledger", "wallet_1", "wallet_2", "100", "PROCESSED", null);
    insertLedgerEntry(transferId, "wallet_1", "DEBIT", "100");

    assertThatThrownBy(() -> insertLedgerEntry(transferId, "wallet_1", "DEBIT", "100"))
        .isInstanceOf(DataIntegrityViolationException.class);

    assertThatCode(() -> insertLedgerEntry(transferId, "wallet_2", "CREDIT", "100"))
        .doesNotThrowAnyException();
  }

  @Test
  void aLedgerEntryMustBelongToARealTransfer() {
    assertThatThrownBy(() -> insertLedgerEntry(UUID.randomUUID(), "wallet_1", "DEBIT", "100"))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  /** The balance CHECK is what stops an overdraft if the service-level check is ever bypassed. */
  @Test
  void aWalletBalanceCannotGoNegative() {
    assertThatThrownBy(() -> jdbc.update("UPDATE wallets SET balance = -1 WHERE id = 'wallet_1'"))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  @Test
  void aWalletStatusMustBeOneOfTheKnownValues() {
    assertThatThrownBy(
            () -> jdbc.update("UPDATE wallets SET status = 'FROZEN' WHERE id = 'wallet_1'"))
        .isInstanceOf(DataIntegrityViolationException.class);
  }

  private void insertTransfer(
      UUID id,
      String key,
      String fromId,
      String toId,
      String amount,
      String status,
      String failureReason) {
    jdbc.update(
        """
        INSERT INTO transfers (
            id, idempotency_key, from_wallet_id, to_wallet_id,
            initiated_by, amount, status, failure_reason)
        VALUES (?, ?, ?, ?, ?, ?::numeric, ?, ?)
        """,
        id,
        key,
        fromId,
        toId,
        CUSTOMER,
        amount,
        status,
        failureReason);
  }

  private void insertLedgerEntry(UUID transferId, String walletId, String type, String amount) {
    jdbc.update(
        """
        INSERT INTO ledger_entries (id, transfer_id, customer_id, wallet_id, type, amount)
        VALUES (?, ?, ?, ?, ?, ?::numeric)
        """,
        UUID.randomUUID(),
        transferId,
        CUSTOMER,
        walletId,
        type,
        amount);
  }
}
