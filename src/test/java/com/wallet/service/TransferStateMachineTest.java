package com.wallet.service;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import com.wallet.exception.IllegalStateTransitionException;
import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class TransferStateMachineTest {

    @Test
    void newTransferStartsPending() {
        Transfer t = Transfer.createPending("key1", "w1", "w2", new BigDecimal("10.00"));
        assertThat(t.getStatus()).isEqualTo(TransferStatus.PENDING);
    }

    @Test
    void pendingCanTransitionToProcessed() {
        Transfer t = Transfer.createPending("key1", "w1", "w2", new BigDecimal("10.00"));
        t.markProcessed();
        assertThat(t.getStatus()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void pendingCanTransitionToFailed() {
        Transfer t = Transfer.createPending("key1", "w1", "w2", new BigDecimal("10.00"));
        t.markFailed("insufficient balance");
        assertThat(t.getStatus()).isEqualTo(TransferStatus.FAILED);
        assertThat(t.getFailureReason()).isEqualTo("insufficient balance");
    }

    @Test
    void processedIsTerminalAndCannotTransitionAgain() {
        Transfer t = Transfer.createPending("key1", "w1", "w2", new BigDecimal("10.00"));
        t.markProcessed();
//        assertThatThrownBy(t::markProcessed).isInstanceOf(IllegalStateTransitionException.class);
        assertThatThrownBy(() -> t.markFailed("late failure")).isInstanceOf(IllegalStateTransitionException.class);
    }

    @Test
    void failedIsTerminalAndCannotTransitionAgain() {
        Transfer t = Transfer.createPending("key1", "w1", "w2", new BigDecimal("10.00"));
        t.markFailed("insufficient balance");
        assertThatThrownBy(t::markProcessed).isInstanceOf(IllegalStateTransitionException.class);
    }

    @Test
    void rejectsSameSourceAndDestinationWallet() {
        assertThatThrownBy(() -> Transfer.createPending("key1", "w1", "w1", new BigDecimal("10.00")))
                .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void rejectsNonPositiveAmount() {
        assertThatThrownBy(() -> Transfer.createPending("key1", "w1", "w2", BigDecimal.ZERO))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Transfer.createPending("key1", "w1", "w2", new BigDecimal("-1")))
                .isInstanceOf(IllegalArgumentException.class);
    }
}
