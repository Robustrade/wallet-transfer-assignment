package com.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.math.BigDecimal;
import org.junit.jupiter.api.Test;

class TransferTest {

    @Test
    void pending_createsTransferInPendingState() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PENDING);
        assertThat(transfer.isPending()).isTrue();
        assertThat(transfer.getIdempotencyKey()).isEqualTo("key-1");
        assertThat(transfer.getFromWalletId()).isEqualTo("w1");
        assertThat(transfer.getToWalletId()).isEqualTo("w2");
        assertThat(transfer.getAmount()).isEqualByComparingTo("100.00");
    }

    @Test
    void markProcessed_fromPending_updatesStatus() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        transfer.markProcessed();
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(transfer.isPending()).isFalse();
    }

    @Test
    void markFailed_fromPending_updatesStatusAndReason() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        transfer.markFailed("Insufficient funds");
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
        assertThat(transfer.getFailureReason()).isEqualTo("Insufficient funds");
    }

    @Test
    void markProcessed_fromProcessed_throwsIllegalStateException() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        transfer.markProcessed();
        assertThatThrownBy(transfer::markProcessed)
            .isInstanceOf(IllegalStateException.class)
            .hasMessageContaining("PROCESSED");
    }

    @Test
    void markProcessed_fromFailed_throwsIllegalStateException() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        transfer.markFailed("error");
        assertThatThrownBy(transfer::markProcessed)
            .isInstanceOf(IllegalStateException.class);
    }

    @Test
    void markFailed_fromProcessed_throwsIllegalStateException() {
        Transfer transfer = Transfer.pending("key-1", "w1", "w2", new BigDecimal("100.00"));
        transfer.markProcessed();
        assertThatThrownBy(() -> transfer.markFailed("late failure"))
            .isInstanceOf(IllegalStateException.class);
    }
}
