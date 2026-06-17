package com.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class TransferStatusTest {

    @Test
    void allowsPendingToProcessedOrFailed() {
        assertThat(TransferStatus.PENDING.canTransitionTo(TransferStatus.PROCESSED)).isTrue();
        assertThat(TransferStatus.PENDING.canTransitionTo(TransferStatus.FAILED)).isTrue();
    }

    @Test
    void disallowsTransitionsFromTerminalStates() {
        assertThat(TransferStatus.PROCESSED.canTransitionTo(TransferStatus.FAILED)).isFalse();
        assertThat(TransferStatus.FAILED.canTransitionTo(TransferStatus.PROCESSED)).isFalse();
    }
}
