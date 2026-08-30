package com.example.wallettransfer.domain;

import org.junit.jupiter.api.Test;
import org.springframework.test.util.ReflectionTestUtils;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class WalletTest {

    @Test
    void appliesDebitAndCreditWithoutLosingDecimalPrecision() {
        Wallet wallet = wallet("10.0000");

        wallet.debit(new BigDecimal("2.5000"));
        wallet.credit(new BigDecimal("0.1250"));

        assertThat(wallet.getBalance()).isEqualByComparingTo("7.6250");
    }

    @Test
    void rejectsDebitThatWouldMakeBalanceNegative() {
        Wallet wallet = wallet("10");

        assertThatThrownBy(() -> wallet.debit(new BigDecimal("10.01")))
                .isInstanceOf(IllegalStateException.class);
    }

    @Test
    void rejectsZeroOrNullAmounts() {
        Wallet wallet = wallet("10");

        assertThatThrownBy(() -> wallet.credit(BigDecimal.ZERO)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> wallet.debit(null)).isInstanceOf(IllegalArgumentException.class);
    }

    private Wallet wallet(String balance) {
        Wallet wallet = new Wallet();
        ReflectionTestUtils.setField(wallet, "balance", new BigDecimal(balance));
        return wallet;
    }
}
