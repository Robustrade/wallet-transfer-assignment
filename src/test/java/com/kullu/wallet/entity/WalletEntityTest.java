package com.kullu.wallet.entity;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class WalletEntityTest {

    @Test
    void initialBalanceCannotBeNegative() {
        assertThatThrownBy(() -> new WalletEntity("wallet-a", -1))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void debitReducesBalance() {
        WalletEntity w = new WalletEntity("wallet-a", 1_000);
        w.debit(400);
        assertThat(w.getBalance()).isEqualTo(600);
    }

    @Test
    void creditIncreasesBalance() {
        WalletEntity w = new WalletEntity("wallet-a", 1_000);
        w.credit(250);
        assertThat(w.getBalance()).isEqualTo(1_250);
    }

    @Test
    void debitRejectsOverdraft() {
        WalletEntity w = new WalletEntity("wallet-a", 100);
        assertThatThrownBy(() -> w.debit(101)).isInstanceOf(IllegalStateException.class);
    }

    @Test
    void amountsMustBePositive() {
        WalletEntity w = new WalletEntity("wallet-a", 100);
        assertThatThrownBy(() -> w.debit(0)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> w.credit(-1)).isInstanceOf(IllegalArgumentException.class);
    }
}
