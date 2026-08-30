package com.example.wallettransfer.domain;

import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class TransferStateMachineTest {

    @Test
    void pendingTransferCanMoveToProcessed() {
        Transfer transfer = Transfer.pending("w1", "w2", new BigDecimal("10"));

        transfer.markProcessed();

        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(transfer.getCompletedAt()).isNotNull();
    }

    @Test
    void terminalTransferCannotTransitionAgain() {
        Transfer transfer = Transfer.pending("w1", "w2", new BigDecimal("10"));
        transfer.markFailed("INSUFFICIENT_FUNDS");

        assertThatThrownBy(transfer::markProcessed)
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("FAILED");
    }

    @Test
    void pendingTransferRejectsInvalidWalletsAndAmounts() {
        assertThatThrownBy(() -> Transfer.pending(null, "w2", BigDecimal.ONE))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Transfer.pending("w1", "w1", BigDecimal.ONE))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Transfer.pending("w1", "w2", BigDecimal.ZERO))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Transfer.pending("w1", null, BigDecimal.ONE))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Transfer.pending("w1", "w2", null))
                .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void failedTransitionRequiresAReason() {
        Transfer transfer = Transfer.pending("w1", "w2", BigDecimal.ONE);

        assertThatThrownBy(() -> transfer.markFailed(" "))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("reason");
    }
}
