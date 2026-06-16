package com.wallet.exception;

/**
 * Raised when the executor's idempotency claim repeatedly observes a row that
 * is then absent on read — symptom of a peer transaction that started, was
 * rolled back, and another peer that won the race in the same window. The
 * caller did <em>not</em> reuse the key with a different body (that case still
 * surfaces as {@link IdempotencyConflictException} → 409); the system has
 * thrashed past its retry budget and the request is being shed.
 *
 * <p>Mapped to HTTP 503 Service Unavailable with a {@code Retry-After} header
 * so clients (and ops dashboards) can distinguish "you sent me a bad request"
 * from "the system is hot, back off and try again". Using 409 here would
 * mislead anyone debugging a hot-shard incident — the wire response would
 * point at the caller when the cause is server-side contention.
 */
public class TransientIdempotencyConflictException extends RuntimeException {

    /**
     * Discriminates the wire message and ops-log reason without changing the
     * 503 status — both are server-side transient failures, but the dashboard
     * stories are different (retry-loop saturation vs. request-thread
     * interruption mid-backoff). Named {@code Reason} (not {@code Cause}) to
     * avoid shadowing {@link Throwable#getCause()}. Adding a value here is
     * safe; existing dashboards that key only off the 503 status remain
     * unaffected.
     */
    public enum Reason {
        /** Retry budget exhausted because peers kept claiming-and-rolling-back the slot. */
        RETRY_BUDGET_EXHAUSTED("retry budget exhausted"),
        /** The request thread was interrupted (e.g. servlet container graceful-shutdown) while sleeping between retries. */
        REQUEST_INTERRUPTED("request thread interrupted before retry budget exhausted");

        private final String description;
        Reason(String description) { this.description = description; }
        public String description() { return description; }
    }

    private final String idempotencyKey;
    private final int retryAfterSeconds;
    private final Reason reason;

    public TransientIdempotencyConflictException(String idempotencyKey, int retryAfterSeconds) {
        this(idempotencyKey, retryAfterSeconds, Reason.RETRY_BUDGET_EXHAUSTED);
    }

    public TransientIdempotencyConflictException(String idempotencyKey, int retryAfterSeconds, Reason reason) {
        super("Transient idempotency contention for key '" + idempotencyKey
                + "': " + reason.description() + ". Please retry after a brief delay.");
        this.idempotencyKey = idempotencyKey;
        this.retryAfterSeconds = retryAfterSeconds;
        this.reason = reason;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public int getRetryAfterSeconds() {
        return retryAfterSeconds;
    }

    public Reason getReason() {
        return reason;
    }
}
