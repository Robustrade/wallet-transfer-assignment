package com.wallettransfer.repository.jdbc;

import static org.assertj.core.api.Assertions.assertThatCode;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.dao.DataAccessException;
import org.springframework.transaction.support.TransactionTemplate;

/** The database itself must reject invalid data, even if application code is wrong. */
class SchemaTest extends PostgresIntegrationTest {

  private static final String INSERT_TRANSFER =
      """
      INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state, failure_reason,
                             created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, now(), now())
      """;
  private static final String INSERT_LEDGER =
      """
      INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
      VALUES (?, ?, ?, ?, ?, now())
      """;
  private static final String INSERT_KEY =
      "INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at)"
          + " VALUES (?, 'h', ?, now())";

  @Autowired private TransactionTemplate tx;

  @BeforeEach
  void seedWallets() {
    for (String id : new String[] {"w1", "w2"}) {
      jdbc.update(
          "INSERT INTO wallets (id, balance, opening_balance, created_at, updated_at)"
              + " VALUES (?, 100, 100, now(), now())",
          id);
    }
  }

  @Test
  void walletBalanceCannotGoNegative() {
    assertThatThrownBy(() -> jdbc.update("UPDATE wallets SET balance = -1 WHERE id = 'w1'"))
        .isInstanceOf(DataAccessException.class);
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource(
      nullValues = "NULL",
      value = {
        "zero amount,w1,w2,0,PENDING,NULL",
        "same wallet,w1,w1,10,PENDING,NULL",
        "unknown state,w1,w2,10,DONE,NULL",
        "unknown source wallet,nope,w2,10,PENDING,NULL",
        "failed without reason,w1,w2,10,FAILED,NULL",
        "reason on a non-failed transfer,w1,w2,10,PROCESSED,INSUFFICIENT_FUNDS"
      })
  void invalidTransfersAreRejected(
      String name, String from, String to, long amount, String state, String reason) {
    assertThatThrownBy(() -> jdbc.update(INSERT_TRANSFER, "t1", from, to, amount, state, reason))
        .isInstanceOf(DataAccessException.class);
  }

  @Test
  void validTransfersAreAccepted() {
    assertThatCode(() -> jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null))
        .doesNotThrowAnyException();
    assertThatCode(
            () ->
                jdbc.update(INSERT_TRANSFER, "t2", "w1", "w2", 10, "FAILED", "INSUFFICIENT_FUNDS"))
        .doesNotThrowAnyException();
  }

  @Test
  void ledgerAllowsOneDebitAndOneCreditPerTransfer() {
    jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null);
    jdbc.update(INSERT_LEDGER, "e1", "t1", "w1", "DEBIT", 10);

    assertThatThrownBy(() -> jdbc.update(INSERT_LEDGER, "e2", "t1", "w1", "DEBIT", 10))
        .as("a second DEBIT for the same transfer")
        .isInstanceOf(DataAccessException.class);
    assertThatCode(() -> jdbc.update(INSERT_LEDGER, "e3", "t1", "w2", "CREDIT", 10))
        .doesNotThrowAnyException();
  }

  @Test
  void ledgerEntriesNeedAnExistingTransferAndPositiveAmount() {
    jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null);

    assertThatThrownBy(() -> jdbc.update(INSERT_LEDGER, "e1", "missing", "w1", "DEBIT", 10))
        .isInstanceOf(DataAccessException.class);
    assertThatThrownBy(() -> jdbc.update(INSERT_LEDGER, "e2", "t1", "w1", "DEBIT", 0))
        .isInstanceOf(DataAccessException.class);
  }

  @Test
  void ledgerEntriesAreImmutable() {
    jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null);
    jdbc.update(INSERT_LEDGER, "e1", "t1", "w1", "DEBIT", 10);

    assertThatThrownBy(() -> jdbc.update("UPDATE ledger_entries SET amount = 99 WHERE id = 'e1'"))
        .isInstanceOf(DataAccessException.class);
    assertThatThrownBy(() -> jdbc.update("DELETE FROM ledger_entries WHERE id = 'e1'"))
        .isInstanceOf(DataAccessException.class);
  }

  @Test
  void idempotencyKeyIsUniqueAndBoundToOneTransfer() {
    jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null);
    jdbc.update(INSERT_TRANSFER, "t2", "w1", "w2", 10, "PENDING", null);
    jdbc.update(INSERT_KEY, "k1", "t1");

    assertThatThrownBy(() -> jdbc.update(INSERT_KEY, "k1", "t2"))
        .as("the same key cannot be claimed twice")
        .isInstanceOf(DataAccessException.class);
    assertThatThrownBy(() -> jdbc.update(INSERT_KEY, "k2", "t1"))
        .as("one transfer has at most one key")
        .isInstanceOf(DataAccessException.class);
  }

  @Test
  void keyCanBeClaimedBeforeItsTransferWithinOneTransaction() {
    assertThatCode(
            () ->
                tx.executeWithoutResult(
                    status -> {
                      jdbc.update(INSERT_KEY, "k1", "t1");
                      jdbc.update(INSERT_TRANSFER, "t1", "w1", "w2", 10, "PENDING", null);
                    }))
        .doesNotThrowAnyException();
  }

  @Test
  void aClaimedKeyWithoutATransferCannotCommit() {
    assertThatThrownBy(() -> tx.executeWithoutResult(status -> jdbc.update(INSERT_KEY, "k1", "t1")))
        .isInstanceOf(DataAccessException.class);
  }
}
