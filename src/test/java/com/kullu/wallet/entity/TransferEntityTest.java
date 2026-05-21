package com.kullu.wallet.entity;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.UUID;

import org.junit.jupiter.api.Test;

class TransferEntityTest {

    private final String from = "wallet-a";
    private final String to = "wallet-b";

    @Test
    void cannotCreateSelfTransfer() {
        assertThatThrownBy(() -> new TransferEntity(UUID.randomUUID(), from, from, 100))
            .isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("from and to wallets must differ");
    }

    @Test
    void cannotCreateWithNonPositiveAmount() {
        assertThatThrownBy(() -> new TransferEntity(UUID.randomUUID(), from, to, 0))
            .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new TransferEntity(UUID.randomUUID(), from, to, -1))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void newTransferStartsInPending() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        assertThat(t.getStatus()).isEqualTo(TransferStatus.PENDING);
        assertThat(t.getFailureReason()).isNull();
    }

    @Test
    void canTransitionPendingToProcessed() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        t.markProcessed();
        assertThat(t.getStatus()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void canTransitionPendingToFailed() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        t.markFailed("INSUFFICIENT_FUNDS");
        assertThat(t.getStatus()).isEqualTo(TransferStatus.FAILED);
        assertThat(t.getFailureReason()).isEqualTo("INSUFFICIENT_FUNDS");
    }

    @Test
    void cannotTransitionFromProcessed() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        t.markProcessed();
        assertThatThrownBy(() -> t.markProcessed()).isInstanceOf(IllegalStateException.class);
        assertThatThrownBy(() -> t.markFailed("X")).isInstanceOf(IllegalStateException.class);
    }

    @Test
    void cannotTransitionFromFailed() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        t.markFailed("X");
        assertThatThrownBy(() -> t.markProcessed()).isInstanceOf(IllegalStateException.class);
        assertThatThrownBy(() -> t.markFailed("Y")).isInstanceOf(IllegalStateException.class);
    }

    @Test
    void failedRequiresReason() {
        TransferEntity t = new TransferEntity(UUID.randomUUID(), from, to, 100);
        assertThatThrownBy(() -> t.markFailed(null)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> t.markFailed("  ")).isInstanceOf(IllegalArgumentException.class);
    }
}
