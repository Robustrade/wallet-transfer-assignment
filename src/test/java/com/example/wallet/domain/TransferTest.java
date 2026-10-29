package com.example.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class TransferTest {

    private Transfer pending() {
        return Transfer.pending("key-1", "hash", "wallet_1", "wallet_2", 100);
    }

    @Test
    void newTransferStartsPending() {
        assertThat(pending().status()).isEqualTo(TransferStatus.PENDING);
    }

    @Test
    void pendingCanBeProcessed() {
        assertThat(pending().markProcessed().status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void pendingCanFailWithReason() {
        Transfer failed = pending().markFailed("INSUFFICIENT_FUNDS");
        assertThat(failed.status()).isEqualTo(TransferStatus.FAILED);
        assertThat(failed.failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
    }

    @Test
    void terminalStatesCannotChangeAgain() {
        Transfer processed = pending().markProcessed();
        Transfer failed = pending().markFailed("x");

        assertThatThrownBy(processed::markProcessed).isInstanceOf(IllegalTransferStateException.class);
        assertThatThrownBy(() -> processed.markFailed("x")).isInstanceOf(IllegalTransferStateException.class);
        assertThatThrownBy(failed::markProcessed).isInstanceOf(IllegalTransferStateException.class);
    }

    @Test
    void rejectsNonPositiveAmount() {
        assertThatThrownBy(() -> Transfer.pending("k", "h", "a", "b", 0))
                .isInstanceOf(InvalidTransferException.class);
        assertThatThrownBy(() -> Transfer.pending("k", "h", "a", "b", -5))
                .isInstanceOf(InvalidTransferException.class);
    }

    @Test
    void rejectsSameSourceAndDestination() {
        assertThatThrownBy(() -> Transfer.pending("k", "h", "a", "a", 10))
                .isInstanceOf(InvalidTransferException.class);
    }
}
