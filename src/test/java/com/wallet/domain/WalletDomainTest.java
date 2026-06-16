package com.wallet.domain;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

/**
 * Unit tests for Wallet domain entity.
 * Focuses on debit/credit operations and validation.
 */
class WalletDomainTest {

    @Nested
    @DisplayName("Wallet debit")
    class Debit {

        @Test
        @DisplayName("Should debit successfully with sufficient balance")
        void shouldDebitSuccessfully() {
            Wallet wallet = new Wallet("w1", 1000L);

            wallet.debit(300L);

            assertThat(wallet.getBalance()).isEqualTo(700L);
        }

        @Test
        @DisplayName("Should debit entire balance")
        void shouldDebitEntireBalance() {
            Wallet wallet = new Wallet("w1", 500L);

            wallet.debit(500L);

            assertThat(wallet.getBalance()).isEqualTo(0L);
        }

        @Test
        @DisplayName("Should reject debit with insufficient balance")
        void shouldRejectInsufficientBalance() {
            Wallet wallet = new Wallet("w1", 100L);

            assertThatThrownBy(() -> wallet.debit(200L))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("Insufficient balance");
        }

        @Test
        @DisplayName("Should reject zero debit amount")
        void shouldRejectZeroDebit() {
            Wallet wallet = new Wallet("w1", 100L);

            assertThatThrownBy(() -> wallet.debit(0L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }

        @Test
        @DisplayName("Should reject negative debit amount")
        void shouldRejectNegativeDebit() {
            Wallet wallet = new Wallet("w1", 100L);

            assertThatThrownBy(() -> wallet.debit(-50L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }
    }

    @Nested
    @DisplayName("Wallet credit")
    class Credit {

        @Test
        @DisplayName("Should credit successfully")
        void shouldCreditSuccessfully() {
            Wallet wallet = new Wallet("w1", 1000L);

            wallet.credit(500L);

            assertThat(wallet.getBalance()).isEqualTo(1500L);
        }

        @Test
        @DisplayName("Should credit zero-balance wallet")
        void shouldCreditZeroBalance() {
            Wallet wallet = new Wallet("w1", 0L);

            wallet.credit(250L);

            assertThat(wallet.getBalance()).isEqualTo(250L);
        }

        @Test
        @DisplayName("Should reject zero credit amount")
        void shouldRejectZeroCredit() {
            Wallet wallet = new Wallet("w1", 100L);

            assertThatThrownBy(() -> wallet.credit(0L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }

        @Test
        @DisplayName("Should reject negative credit amount")
        void shouldRejectNegativeCredit() {
            Wallet wallet = new Wallet("w1", 100L);

            assertThatThrownBy(() -> wallet.credit(-50L))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
        }
    }
}
