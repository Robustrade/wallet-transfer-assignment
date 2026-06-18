package com.wallet.domain;

import static org.assertj.core.api.Assertions.assertThat;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import org.junit.jupiter.api.Test;

class WalletTest {

    @Test
    void hasAtLeast_trueWhenBalanceEqualsAmount() {
        Wallet wallet = new Wallet(UUID.randomUUID(), new BigDecimal("100.00"), Instant.now(), Instant.now());

        assertThat(wallet.hasAtLeast(new BigDecimal("100.00"))).isTrue();
    }

    @Test
    void hasAtLeast_falseWhenBalanceBelowAmount() {
        Wallet wallet = new Wallet(UUID.randomUUID(), new BigDecimal("99.99"), Instant.now(), Instant.now());

        assertThat(wallet.hasAtLeast(new BigDecimal("100.00"))).isFalse();
    }
}
