package com.example.wallettransfer.domain;

import org.springframework.test.util.ReflectionTestUtils;

import java.math.BigDecimal;
import java.time.Instant;

public final class WalletFixtures {

    private WalletFixtures() {
    }

    public static Wallet wallet(String id, String balance) {
        Wallet wallet = new Wallet();
        ReflectionTestUtils.setField(wallet, "id", id);
        ReflectionTestUtils.setField(wallet, "balance", new BigDecimal(balance));
        ReflectionTestUtils.setField(wallet, "createdAt", Instant.parse("2026-01-01T00:00:00Z"));
        return wallet;
    }
}
