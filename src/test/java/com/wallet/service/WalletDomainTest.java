package com.wallet.service;

import com.wallet.domain.Wallet;
import com.wallet.exception.InsufficientBalanceException;
import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class WalletDomainTest {

    @Test
    void debitReducesBalance() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        wallet.debit(new BigDecimal("40.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("60.00");
    }

    @Test
    void creditIncreasesBalance() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        wallet.credit(new BigDecimal("40.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("140.00");
    }

    @Test
    void debitBeyondBalanceThrowsAndLeavesBalanceUnchanged() {
        Wallet wallet = new Wallet("w1", new BigDecimal("10.00"));
        assertThatThrownBy(() -> wallet.debit(new BigDecimal("10.01")))
                .isInstanceOf(InsufficientBalanceException.class);
        assertThat(wallet.getBalance()).isEqualByComparingTo("10.00");
    }

    @Test
    void debitExactBalanceIsAllowedAndLeavesZero() {
        Wallet wallet = new Wallet("w1", new BigDecimal("10.00"));
        wallet.debit(new BigDecimal("10.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("0.00");
    }

    @Test
    void nonPositiveDebitIsRejected() {
        Wallet wallet = new Wallet("w1", new BigDecimal("10.00"));
        assertThatThrownBy(() -> wallet.debit(BigDecimal.ZERO)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> wallet.debit(new BigDecimal("-5.00"))).isInstanceOf(IllegalArgumentException.class);
    }
}
