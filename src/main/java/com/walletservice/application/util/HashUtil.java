package com.walletservice.application.util;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Base64;

/**
 * Utility for generating deterministic hashes of request parameters.
 * Used in idempotency detection to ensure the same key is not reused with different parameters.
 */
public class HashUtil {
    private static final String HASH_ALGORITHM = "SHA256";

    /**
     * Generates a SHA256 hash of the request data.
     * Used to detect if the same idempotency key is used with different request bodies.
     */
    public static String hashRequest(String data) {
        try {
            MessageDigest digest = MessageDigest.getInstance(HASH_ALGORITHM);
            byte[] hash = digest.digest(data.getBytes(StandardCharsets.UTF_8));
            return Base64.getEncoder().encodeToString(hash);
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException("SHA256 algorithm not available", e);
        }
    }

    /**
     * Creates a deterministic string representation of transfer request parameters.
     */
    public static String hashTransferRequest(String fromWalletId, String toWalletId, Long amount) {
        String concatenated = String.format("%s:%s:%d", fromWalletId, toWalletId, amount);
        return hashRequest(concatenated);
    }
}

