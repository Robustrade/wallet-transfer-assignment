package com.wallet.util;

/**
 * Masking helpers for log statements. Applied at every site where a
 * caller-supplied identifier — whose full value is not required at log-read
 * time — would otherwise be written to a log file.
 *
 * <p>Today the discipline is enforced for {@code Idempotency-Key} values: the
 * full key is persisted in {@code idempotency_records} and echoed in error
 * responses, so log readers can always reconstruct it from the DB by
 * {@code clientId} + the masked prefix/suffix. The masked form preserves
 * enough to correlate retries of the same key in the same log file without
 * leaking the full value if logs are exfiltrated.
 *
 * <p><strong>Not applied to:</strong> {@code clientId}, {@code walletId},
 * {@code amount}, {@code transferId}. These are the primary correlation
 * handles for debugging and are already echoed in error responses / DB
 * rows; masking them would harm investigations without buying privacy.
 *
 * <p><strong>Never log at all:</strong> HMAC signatures, secrets, raw
 * request bodies, the {@code SIGNATURE_SECRET} property. {@link #mask}
 * is not a substitute for "do not log this value" — if you find a log
 * line emitting any of those, delete the line, do not just mask it.
 */
public final class LogSafe {

    private static final String MASK = "***";
    /** Inputs shorter than this render as {@value #MASK} in full — there is no
     *  meaningful prefix/suffix to retain. Twelve chars covers UUIDs and ULIDs
     *  with margin; shorter keys are unusual and treated as opaque. */
    private static final int MIN_VISIBLE_LENGTH = 12;

    private LogSafe() {}

    /**
     * Returns a redacted form of {@code value}: first 4 + {@value #MASK} +
     * last 4 chars when {@code value.length() >= 12}; otherwise just
     * {@value #MASK}. {@code null} renders as the literal {@code "null"} so
     * an absent value is still visible in the log line (otherwise an
     * accidental null would be indistinguishable from a redacted value).
     */
    public static String mask(String value) {
        if (value == null) {
            return "null";
        }
        if (value.length() < MIN_VISIBLE_LENGTH) {
            return MASK;
        }
        return value.substring(0, 4) + MASK + value.substring(value.length() - 4);
    }
}
