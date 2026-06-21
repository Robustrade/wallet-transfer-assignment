package com.wallet.transfer.domain;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

@DisplayName("Transfer domain model")
class TransferTest {

    @Test
    @DisplayName("new transfer should be in PENDING state")
    void initialState() {
        Transfer transfer = new Transfer("key-1", "from", "to", 100L);
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PENDING);
    }

    @Test
    @DisplayName("PENDING transfer can transition to PROCESSED")
    void pendingToProcessed() {
        Transfer transfer = new Transfer("key-1", "from", "to", 100L);
        transfer.markProcessed();
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    @DisplayName("PENDING transfer can transition to FAILED")
    void pendingToFailed() {
        Transfer transfer = new Transfer("key-1", "from", "to", 100L);
        transfer.markFailed(0L);
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
    }

    @Test
    @DisplayName("PROCESSED transfer cannot transition again")
    void processedIsTerminal() {
        Transfer transfer = new Transfer("key-1", "from", "to", 100L);
        transfer.markProcessed();

        assertThatThrownBy(transfer::markProcessed)
                .isInstanceOf(IllegalStateException.class);
        assertThatThrownBy(() -> transfer.markFailed(0L))
                .isInstanceOf(IllegalStateException.class);
    }

    @Test
    @DisplayName("FAILED transfer cannot transition again")
    void failedIsTerminal() {
        Transfer transfer = new Transfer("key-1", "from", "to", 100L);
        transfer.markFailed(0L);

        assertThatThrownBy(transfer::markProcessed)
                .isInstanceOf(IllegalStateException.class);
        assertThatThrownBy(() -> transfer.markFailed(0L))
                .isInstanceOf(IllegalStateException.class);
    }
}
