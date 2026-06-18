package com.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import org.junit.jupiter.api.Test;

class TransferTest {

    private Transfer pendingTransfer() {
        return Transfer.createPending(UUID.randomUUID(), "key-1", "fingerprint-1", UUID.randomUUID(),
                UUID.randomUUID(), BigDecimal.TEN, Instant.now());
    }

    @Test
    void createPending_startsInPendingStatus() {
        Transfer transfer = pendingTransfer();

        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PENDING);
        assertThat(transfer.getFailureReason()).isNull();
    }

    @Test
    void markProcessed_fromPending_succeeds() {
        Transfer transfer = pendingTransfer();

        transfer.markProcessed(Instant.now());

        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(transfer.getFailureReason()).isNull();
    }

    @Test
    void markFailed_fromPending_succeeds() {
        Transfer transfer = pendingTransfer();

        transfer.markFailed("insufficient funds", Instant.now());

        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
        assertThat(transfer.getFailureReason()).isEqualTo("insufficient funds");
    }

    @Test
    void markProcessed_onTerminalState_throws() {
        Transfer transfer = pendingTransfer();
        transfer.markProcessed(Instant.now());

        assertThatThrownBy(() -> transfer.markProcessed(Instant.now()))
                .isInstanceOf(IllegalStateException.class);
    }

    @Test
    void markFailed_onTerminalState_throws() {
        Transfer transfer = pendingTransfer();
        transfer.markFailed("insufficient funds", Instant.now());

        assertThatThrownBy(() -> transfer.markFailed("again", Instant.now()))
                .isInstanceOf(IllegalStateException.class);
    }

    @Test
    void constructor_rejectsSameSourceAndDestinationWallet() {
        UUID walletId = UUID.randomUUID();

        assertThatThrownBy(() -> Transfer.createPending(UUID.randomUUID(), "key", "fp", walletId, walletId,
                BigDecimal.TEN, Instant.now())).isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void constructor_rejectsNonPositiveAmount() {
        assertThatThrownBy(() -> Transfer.createPending(UUID.randomUUID(), "key", "fp", UUID.randomUUID(),
                UUID.randomUUID(), BigDecimal.ZERO, Instant.now())).isInstanceOf(IllegalArgumentException.class);
    }
}
