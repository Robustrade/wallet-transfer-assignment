package com.wallettransfer.service;

import com.wallettransfer.web.dto.CreateTransferRequest;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/**
 * Builds a stable fingerprint of a transfer request for idempotency checks.
 * Same from/to/amount must produce the same hash; different payloads must differ.
 */
final class RequestHasher {

    private RequestHasher() {}

    /**
     * Computes SHA-256 hex digest of {@code fromWalletId|toWalletId|amount}.
     *
     * @param request transfer request to fingerprint
     * @return lowercase hex hash string
     */
    static String hash(CreateTransferRequest request) {
        String payload = request.fromWalletId() + "|" + request.toWalletId() + "|" + request.amount();
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(payload.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hash);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 not available", e);
        }
    }
}
