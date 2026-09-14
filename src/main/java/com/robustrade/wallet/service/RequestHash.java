package com.robustrade.wallet.service;

import java.math.BigDecimal;
import java.math.RoundingMode;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/**
 * Lightweight stable hash of transfer payload fields for idempotency conflict detection.
 * Uses SHA-256 hex (64 chars) over {@code fromWalletId|toWalletId|amount}, with amount
 * canonicalized to {@code NUMERIC(15,2)} scale so {@code 100.0} and {@code 100.00} match.
 */
public final class RequestHash {

    private static final int MONEY_SCALE = 2;

    private RequestHash() {
    }

    public static String from(String fromWalletId, String toWalletId, BigDecimal amount) {
        String canonicalAmount = canonicalizeAmount(amount).toPlainString();
        String payload = fromWalletId + "|" + toWalletId + "|" + canonicalAmount;
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(payload.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hash);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 not available", e);
        }
    }

    static BigDecimal canonicalizeAmount(BigDecimal amount) {
        if (amount == null) {
            throw new IllegalArgumentException("amount is required");
        }
        return amount.setScale(MONEY_SCALE, RoundingMode.UNNECESSARY);
    }
}
