package com.wallet.domain;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

/**
 * Unit tests for the Transfer domain. Status is fixed at construction —
 * no in-memory state machine — so the tests cover the two factories and
 * the input validation they share.
 */
class TransferDomainTest {

    @Nested
    @DisplayName("Transfer.processed")
    class Processed {

        @Test
        @DisplayName("Should construct PROCESSED transfer with no error message")
        void shouldConstructProcessed() {
            Transfer transfer = Transfer.processed("wallet_1", "wallet_2", 100L);

            assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PROCESSED);
            assertThat(transfer.getFromWalletId()).isEqualTo("wallet_1");
            assertThat(transfer.getToWalletId()).isEqualTo("wallet_2");
            assertThat(transfer.getAmount()).isEqualTo(100L);
            assertThat(transfer.getErrorMessage()).isNull();
        }
    }

    @Nested
    @DisplayName("Transfer.failed")
    class Failed {

        @Test
        @DisplayName("Should construct FAILED transfer with the error message preserved")
        void shouldConstructFailed() {
            Transfer transfer = Transfer.failed("wallet_1", "wallet_2", 100L, "Insufficient balance");

            assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
            assertThat(transfer.getErrorMessage()).isEqualTo("Insufficient balance");
        }
    }

    @Nested
    @DisplayName("Input validation (shared by both factories)")
    class Validation {

        @Test
        @DisplayName("Should reject same source and destination wallet")
        void shouldRejectSameWallet() {
            assertThatThrownBy(() -> Transfer.processed("wallet_1", "wallet_1", 100L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("same wallet");
        }

        @Test
        @DisplayName("Should reject zero amount")
        void shouldRejectZeroAmount() {
            assertThatThrownBy(() -> Transfer.processed("wallet_1", "wallet_2", 0L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }

        @Test
        @DisplayName("Should reject negative amount")
        void shouldRejectNegativeAmount() {
            assertThatThrownBy(() -> Transfer.processed("wallet_1", "wallet_2", -50L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }

        @Test
        @DisplayName("Should reject null amount")
        void shouldRejectNullAmount() {
            assertThatThrownBy(() -> Transfer.failed("wallet_1", "wallet_2", null, "err"))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }
    }
}
