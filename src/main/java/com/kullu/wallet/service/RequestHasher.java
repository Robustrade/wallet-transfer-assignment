package com.kullu.wallet.service;

import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/**
 * Deterministic SHA-256 over the business-meaningful fields of a transfer
 * request. Used to detect "same idempotency key, different payload" reuse.
 *
 * <p>Fields are fed into the digest with <strong>length-prefixed framing</strong>
 * (4-byte big-endian length followed by the UTF-8 bytes). A naive
 * "{@code from | to | amount}" delimiter scheme would collide whenever a
 * wallet id legally contains the delimiter character: for example
 * {@code hash("a|b", "c", 100)} would equal {@code hash("a", "b|c", 100)}.
 * Length-prefixed framing makes the canonical encoding unambiguous regardless
 * of the input characters, so the bean-validation rules on
 * {@code CreateTransferRequest} (which permit any non-blank string up to 64
 * characters) cannot be exploited to forge collisions.
 */
public final class RequestHasher {

    private RequestHasher() {}

    public static String hash(String fromWalletId, String toWalletId, long amount) {
        try {
            MessageDigest md = MessageDigest.getInstance("SHA-256");
            updateField(md, fromWalletId);
            updateField(md, toWalletId);
            updateField(md, Long.toString(amount));
            return HexFormat.of().formatHex(md.digest());
        } catch (NoSuchAlgorithmException e) {
            // SHA-256 is mandatory in every JDK; this can never happen.
            throw new IllegalStateException("SHA-256 unavailable", e);
        }
    }

    /**
     * Feed a single field into the digest as {@code <4-byte big-endian length>
     * <UTF-8 bytes>}. The fixed-width length prefix makes the boundary between
     * fields unambiguous and immune to delimiter-injection style collisions.
     */
    private static void updateField(MessageDigest md, String value) {
        byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
        md.update(ByteBuffer.allocate(Integer.BYTES).putInt(bytes.length).array());
        md.update(bytes);
    }
}
