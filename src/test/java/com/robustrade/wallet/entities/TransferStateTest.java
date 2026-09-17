package com.robustrade.wallet.entities;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.TransferStatus;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class TransferStateTest {

  private static Transfer pendingTransfer() {
    return new Transfer(
        "key-1", "wallet_1", "wallet_2", UUID.randomUUID(), new BigDecimal("100.0000"));
  }

  @Test
  void newTransferStartsPending() {
    assertThat(pendingTransfer().getStatus()).isEqualTo(TransferStatus.PENDING);
  }

  @Test
  void pendingTransferCanBeProcessed() {
    Transfer transfer = pendingTransfer();

    transfer.markProcessed();

    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
    assertThat(transfer.getFailureReason()).isNull();
  }

  @Test
  void pendingTransferCanFailWithAReason() {
    Transfer transfer = pendingTransfer();

    transfer.markFailed(FailureReason.INSUFFICIENT_FUNDS);

    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
    assertThat(transfer.getFailureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
  }

  @Test
  void processedTransferIsTerminal() {
    Transfer transfer = pendingTransfer();
    transfer.markProcessed();

    assertThatThrownBy(() -> transfer.markFailed(FailureReason.INSUFFICIENT_FUNDS))
        .isInstanceOf(IllegalStateException.class);
    assertThatThrownBy(transfer::markProcessed).isInstanceOf(IllegalStateException.class);
  }

  @Test
  void failedTransferIsTerminal() {
    Transfer transfer = pendingTransfer();
    transfer.markFailed(FailureReason.WALLET_NOT_ACTIVE);

    assertThatThrownBy(transfer::markProcessed).isInstanceOf(IllegalStateException.class);
  }

  @Test
  void replayMatchIgnoresScaleDifferences() {
    Transfer transfer = pendingTransfer();

    // The stored NUMERIC(15,4) reads back as 100.0000 while the request parses to 100.
    assertThat(transfer.matches("wallet_1", "wallet_2", new BigDecimal("100"))).isTrue();
    assertThat(transfer.matches("wallet_1", "wallet_2", new BigDecimal("100.01"))).isFalse();
    assertThat(transfer.matches("wallet_9", "wallet_2", new BigDecimal("100"))).isFalse();
  }
}
