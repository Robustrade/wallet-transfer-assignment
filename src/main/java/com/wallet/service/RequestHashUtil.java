package com.wallet.service;

import com.wallet.api.dto.CreateTransferRequest;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

final class RequestHashUtil {

    private RequestHashUtil() {}

    static String hash(CreateTransferRequest request) {
        String payload =
                request.idempotencyKey()
                        + "|"
                        + request.fromWalletId()
                        + "|"
                        + request.toWalletId()
                        + "|"
                        + request.amount();
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(payload.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hash);
        } catch (NoSuchAlgorithmException ex) {
            throw new IllegalStateException("SHA-256 not available", ex);
        }
    }
}
