package com.wallettransfer.repository.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.FailureReason;
import com.wallettransfer.domain.InvalidStateTransitionException;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferState;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

class JdbcTransferRepositoryTest extends PostgresIntegrationTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Autowired private TransferRepository transfers;
  @Autowired private WalletRepository wallets;

  @BeforeEach
  void seedWallets() {
    wallets.create(Wallet.open("w1", 100, NOW));
    wallets.create(Wallet.open("w2", 0, NOW));
  }

  private Transfer pending(String id, String key) {
    return Transfer.create(id, key, "w1", "w2", 40, NOW);
  }

  @Test
  void insertsAndFindsATransfer() {
    transfers.insert(pending("t1", "key-1"));

    Transfer found = transfers.findById("t1").orElseThrow();

    assertThat(found.idempotencyKey()).isEqualTo("key-1");
    assertThat(found.fromWalletId()).isEqualTo("w1");
    assertThat(found.toWalletId()).isEqualTo("w2");
    assertThat(found.amount()).isEqualTo(40);
    assertThat(found.state()).isEqualTo(TransferState.PENDING);
    assertThat(found.failureReason()).isNull();
    assertThat(found.createdAt()).isEqualTo(NOW);
  }

  @Test
  void transferWithoutKeyRoundTripsWithNullKey() {
    transfers.insert(pending("t1", null));

    assertThat(transfers.findById("t1").orElseThrow().idempotencyKey()).isNull();
  }

  @Test
  void unknownTransferIsEmpty() {
    assertThat(transfers.findById("nope")).isEmpty();
  }

  @Test
  void persistsProcessedState() {
    Transfer pending = pending("t1", null);
    transfers.insert(pending);
    Transfer processed = pending.processed(NOW.plusSeconds(1));

    transfers.updateState(processed);

    Transfer stored = transfers.findById("t1").orElseThrow();
    assertThat(stored.state()).isEqualTo(TransferState.PROCESSED);
    assertThat(stored.updatedAt()).isEqualTo(NOW.plusSeconds(1));
  }

  @Test
  void persistsFailedStateWithReason() {
    Transfer pending = pending("t1", null);
    transfers.insert(pending);

    transfers.updateState(pending.failed(FailureReason.INSUFFICIENT_FUNDS, NOW));

    Transfer stored = transfers.findById("t1").orElseThrow();
    assertThat(stored.state()).isEqualTo(TransferState.FAILED);
    assertThat(stored.failureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
  }

  @Test
  void staleWriterCannotOverwriteATerminalState() {
    Transfer pending = pending("t1", null);
    transfers.insert(pending);
    transfers.updateState(pending.processed(NOW)); // the winner

    Transfer stale = pending.failed(FailureReason.INSUFFICIENT_FUNDS, NOW); // loser, old copy

    assertThatThrownBy(() -> transfers.updateState(stale))
        .isInstanceOf(InvalidStateTransitionException.class);
    assertThat(transfers.findById("t1").orElseThrow().state()).isEqualTo(TransferState.PROCESSED);
  }
}
