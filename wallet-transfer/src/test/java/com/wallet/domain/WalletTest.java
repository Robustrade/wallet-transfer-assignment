package com.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallet.exception.InsufficientFundsException;
import java.math.BigDecimal;
import org.junit.jupiter.api.Test;

class WalletTest {

    @Test
    void debit_reducesBalance() {
        Wallet wallet = new Wallet("w1", new BigDecimal("500.00"));
        wallet.debit(new BigDecimal("200.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("300.00");
    }

    @Test
    void debit_exactBalance_reducesToZero() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        wallet.debit(new BigDecimal("100.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("0.00");
    }

    @Test
    void debit_insufficientBalance_throwsInsufficientFundsException() {
        Wallet wallet = new Wallet("w1", new BigDecimal("50.00"));
        assertThatThrownBy(() -> wallet.debit(new BigDecimal("100.00")))
            .isInstanceOf(InsufficientFundsException.class)
            .hasMessageContaining("w1");
    }

    @Test
    void debit_zeroAmount_throwsIllegalArgumentException() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        assertThatThrownBy(() -> wallet.debit(BigDecimal.ZERO))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void debit_negativeAmount_throwsIllegalArgumentException() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        assertThatThrownBy(() -> wallet.debit(new BigDecimal("-10.00")))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void credit_increasesBalance() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        wallet.credit(new BigDecimal("250.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("350.00");
    }

    @Test
    void credit_zeroAmount_throwsIllegalArgumentException() {
        Wallet wallet = new Wallet("w1", new BigDecimal("100.00"));
        assertThatThrownBy(() -> wallet.credit(BigDecimal.ZERO))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void constructor_negativeInitialBalance_throwsIllegalArgumentException() {
        assertThatThrownBy(() -> new Wallet("w1", new BigDecimal("-1.00")))
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void debit_thenCredit_correctBalance() {
        Wallet wallet = new Wallet("w1", new BigDecimal("1000.00"));
        wallet.debit(new BigDecimal("300.00"));
        wallet.credit(new BigDecimal("150.00"));
        assertThat(wallet.getBalance()).isEqualByComparingTo("850.00");
    }
}
