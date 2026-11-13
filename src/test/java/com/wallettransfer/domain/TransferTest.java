package com.wallettransfer.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.time.Instant;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;

class TransferTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Test
  void newTransferStartsPending() {
    Transfer transfer = Transfer.create("t1", "key-1", "wallet_1", "wallet_2", 100, NOW);

    assertThat(transfer.state()).isEqualTo(TransferState.PENDING);
    assertThat(transfer.id()).isEqualTo("t1");
    assertThat(transfer.idempotencyKey()).isEqualTo("key-1");
    assertThat(transfer.fromWalletId()).isEqualTo("wallet_1");
    assertThat(transfer.toWalletId()).isEqualTo("wallet_2");
    assertThat(transfer.amount()).isEqualTo(100);
    assertThat(transfer.failureReason()).isNull();
    assertThat(transfer.createdAt()).isEqualTo(NOW);
    assertThat(transfer.updatedAt()).isEqualTo(NOW);
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource({
    "zero amount,a,b,0,amount must be greater than zero",
    "negative amount,a,b,-5,amount must be greater than zero",
    "missing source,,b,10,source and destination wallet ids are required",
    "missing destination,a,,10,source and destination wallet ids are required",
    "same wallet,a,a,10,source and destination wallets must differ"
  })
  void rejectsInvalidRequests(String name, String from, String to, long amount, String message) {
    assertThatThrownBy(() -> Transfer.create("t1", null, from, to, amount, NOW))
        .isInstanceOf(ValidationException.class)
        .hasMessage(message);
  }

  @Test
  void processedTransitionReturnsNewTransferAndLeavesOriginalUntouched() {
    Transfer pending = Transfer.create("t1", null, "a", "b", 100, NOW);
    Instant later = NOW.plusSeconds(1);

    Transfer processed = pending.processed(later);

    assertThat(processed.state()).isEqualTo(TransferState.PROCESSED);
    assertThat(processed.updatedAt()).isEqualTo(later);
    assertThat(processed.failureReason()).isNull();
    assertThat(pending.state()).isEqualTo(TransferState.PENDING);
  }

  @Test
  void failedTransitionRecordsReason() {
    Transfer failed =
        Transfer.create("t1", null, "a", "b", 100, NOW)
            .failed(FailureReason.INSUFFICIENT_FUNDS, NOW.plusSeconds(1));

    assertThat(failed.state()).isEqualTo(TransferState.FAILED);
    assertThat(failed.failureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
  }

  @Test
  void terminalStatesRejectFurtherTransitions() {
    Transfer pending = Transfer.create("t1", null, "a", "b", 100, NOW);
    Transfer processed = pending.processed(NOW);
    Transfer failed = pending.failed(FailureReason.INSUFFICIENT_FUNDS, NOW);

    for (Transfer terminal : new Transfer[] {processed, failed}) {
      assertThatThrownBy(() -> terminal.processed(NOW))
          .isInstanceOf(InvalidStateTransitionException.class);
      assertThatThrownBy(() -> terminal.failed(FailureReason.INSUFFICIENT_FUNDS, NOW))
          .isInstanceOf(InvalidStateTransitionException.class);
    }
  }
}
