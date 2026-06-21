package com.wallet.transfer.domain;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import com.wallet.transfer.exception.BalanceViolationException;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

@DisplayName("Wallet domain model")
class WalletTest {

    @Test
    @DisplayName("debit should reduce balance")
    void debitReducesBalance() {
        Wallet wallet = new Wallet("w1", 500L);
        wallet.debit(200L);
        assertThat(wallet.getBalance()).isEqualTo(300L);
    }

    @Test
    @DisplayName("debit should reject amount exceeding balance")
    void debitRejectsOverdraft() {
        Wallet wallet = new Wallet("w1", 100L);
        assertThatThrownBy(() -> wallet.debit(200L))
                .isInstanceOf(BalanceViolationException.class)
                .hasMessageContaining("Insufficient funds");
    }

    @Test
    @DisplayName("debit should reject zero or negative amount")
    void debitRejectsNonPositive() {
        Wallet wallet = new Wallet("w1", 100L);
        assertThatThrownBy(() -> wallet.debit(0L))
                .isInstanceOf(BalanceViolationException.class);
        assertThatThrownBy(() -> wallet.debit(-10L))
                .isInstanceOf(BalanceViolationException.class);
    }

    @Test
    @DisplayName("credit should increase balance")
    void creditIncreasesBalance() {
        Wallet wallet = new Wallet("w1", 100L);
        wallet.credit(300L);
        assertThat(wallet.getBalance()).isEqualTo(400L);
    }

    @Test
    @DisplayName("credit should reject zero or negative amount")
    void creditRejectsNonPositive() {
        Wallet wallet = new Wallet("w1", 100L);
        assertThatThrownBy(() -> wallet.credit(0L))
                .isInstanceOf(BalanceViolationException.class);
    }

    @Test
    @DisplayName("credit should reject amount that would overflow balance")
    void creditRejectsOverflow() {
        Wallet wallet = new Wallet("w1", Long.MAX_VALUE - 10);
        assertThatThrownBy(() -> wallet.credit(20L))
                .isInstanceOf(BalanceViolationException.class)
                .hasMessageContaining("overflow");
    }
}
