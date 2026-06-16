package com.wallet.util;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

/**
 * Centralized SHA-256 helpers. Used for idempotency request hashes, HMAC body
 * hashing, and folding strings into 64-bit advisory-lock ids.
 */
public final class Hashing {

    private Hashing() {
    }

    /** Hex-encoded SHA-256 of the UTF-8 bytes of {@code input}. */
    public static String sha256Hex(String input) {
        return HexFormat.of().formatHex(sha256Bytes(input));
    }

    /**
     * Folds the SHA-256 of {@code input} into a 64-bit lock id. Collisions are
     * acceptable for advisory-lock use cases — downstream uniqueness checks
     * (DB constraints) remain authoritative.
     *
     * <p><strong>Collision math.</strong> Postgres advisory locks are keyed by
     * a 64-bit signed integer ({@code bigint}), giving 2^64 ≈ 1.8e19 distinct
     * lock ids. By the birthday bound, two random 64-bit values collide with
     * probability ≈ 50% at √(2^64) ≈ 2^32 ≈ 4.3 billion <em>concurrently
     * held</em> keys — not lifetime keys. The bound assumes a uniform output
     * distribution, which is what truncated SHA-256 gives.
     *
     * <p>In practice, only in-flight (clientId, idempotencyKey) tuples occupy
     * a lock slot — their lock is released when the executing transaction
     * commits or rolls back ({@code pg_try_advisory_xact_lock} in
     * {@code TransferExecutionService.guardConcurrentRequest}). So the
     * concurrency, not the lifetime, sets the collision probability. For any
     * realistic transfer workload the concurrent set is in the thousands at
     * most, putting collision odds in the 1-in-billions range.
     *
     * <p><strong>When this matters.</strong> A collision lets two unrelated
     * keys serialize through the same advisory lock, producing one spurious
     * 409 (the loser's request retries via the
     * {@code idempotency.advisory_lock.rejections} counter — see
     * {@code TransferMetrics}). Durable correctness is preserved by the
     * {@code INSERT ... ON CONFLICT} claim on the canonical (clientId, key)
     * tuple; the metric only exists to make the false-positive case observable.
     * If that counter ever climbs into the per-second range under low overall
     * traffic, this fold is the prime suspect — switch to a 128-bit two-key
     * advisory lock or move the claim earlier so the in-memory lock becomes
     * a pure optimization.
     */
    public static long toLockId(String input) {
        byte[] hashBytes = sha256Bytes(input);
        long result = 0;
        for (int i = 0; i < 8; i++) {
            result = (result << 8) | (hashBytes[i] & 0xff);
        }
        return result;
    }

    private static byte[] sha256Bytes(String input) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            return digest.digest(input.getBytes(StandardCharsets.UTF_8));
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 not available", e);
        }
    }
}
