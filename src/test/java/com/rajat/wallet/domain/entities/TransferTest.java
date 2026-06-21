package com.rajat.wallet.domain.entities;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.rajat.wallet.domain.enums.TransferStatus;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

/**
 * Unit tests for the {@link Transfer} state machine. The guarded transitions are what make state
 * changes safe under retries and duplicates: a transfer may only ever move <em>out of</em> PENDING
 * once, so a replayed or double-processed request can never re-apply a side effect.
 */
class TransferTest {

  private static final UUID FROM = UUID.randomUUID();
  private static final UUID TO = UUID.randomUUID();
  private static final BigDecimal AMOUNT = new BigDecimal("100.00");

  @Test
  void pendingTransferStartsInPendingState() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);

    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PENDING);
    assertThat(transfer.getFromWalletId()).isEqualTo(FROM);
    assertThat(transfer.getToWalletId()).isEqualTo(TO);
    assertThat(transfer.getAmount()).isEqualByComparingTo(AMOUNT);
    assertThat(transfer.getFailureReason()).isNull();
  }

  @Test
  void markProcessedMovesPendingToProcessed() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);

    transfer.markProcessed();

    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
  }

  @Test
  void markFailedMovesPendingToFailedAndRecordsReason() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);

    transfer.markFailed("Insufficient funds");

    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
    assertThat(transfer.getFailureReason()).isEqualTo("Insufficient funds");
  }

  @Test
  void aProcessedTransferCannotBeProcessedAgain() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);
    transfer.markProcessed();

    assertThatThrownBy(transfer::markProcessed).isInstanceOf(IllegalStateException.class);
  }

  @Test
  void aProcessedTransferCannotLaterBeFailed() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);
    transfer.markProcessed();

    assertThatThrownBy(() -> transfer.markFailed("late failure"))
        .isInstanceOf(IllegalStateException.class);
  }

  @Test
  void aFailedTransferCannotLaterBeProcessed() {
    Transfer transfer = Transfer.pending(FROM, TO, AMOUNT);
    transfer.markFailed("Currency mismatch");

    assertThatThrownBy(transfer::markProcessed).isInstanceOf(IllegalStateException.class);
  }
}
