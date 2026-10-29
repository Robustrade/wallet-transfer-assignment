package com.example.wallet.service;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/** Stable SHA-256 of the business fields of a transfer request. */
final class RequestFingerprint {

    private RequestFingerprint() {}

    static String of(CreateTransferCommand c) {
        String canonical = c.fromWalletId() + "|" + c.toWalletId() + "|" + c.amount();
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256").digest(canonical.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(digest);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 is required by the JVM spec", e);
        }
    }
}
