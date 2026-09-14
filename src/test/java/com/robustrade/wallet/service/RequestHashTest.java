package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.math.BigDecimal;
import org.junit.jupiter.api.Test;

class RequestHashTest {

    @Test
    void from_canonicalizesAmountScale_soEquivalentMoneyHashesMatch() {
        String hashPlain = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100"));
        String hashOneDp = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.0"));
        String hashTwoDp = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.00"));

        assertThat(hashPlain).isEqualTo(hashOneDp).isEqualTo(hashTwoDp);
    }

    @Test
    void from_differentAmounts_produceDifferentHashes() {
        String a = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.00"));
        String b = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.01"));

        assertThat(a).isNotEqualTo(b);
    }

    @Test
    void canonicalizeAmount_rejectsValuesThatNeedRoundingBeyondScale2() {
        assertThatThrownBy(() -> RequestHash.canonicalizeAmount(new BigDecimal("1.001")))
                .isInstanceOf(ArithmeticException.class);
    }
}
