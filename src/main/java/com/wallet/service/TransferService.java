package com.wallet.service;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.domain.IdempotencyRecord;
import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.IdempotencyKeyInProgressException;
import com.wallet.exception.InsufficientBalanceException;
import com.wallet.exception.TransientIdempotencyConflictException;
import com.wallet.exception.UnknownClientException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.metrics.TransferMetrics;
import com.wallet.repository.ClientRepository;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.util.Hashing;
import com.wallet.util.LogSafe;
import io.micrometer.core.instrument.Timer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;

import java.util.concurrent.ThreadLocalRandom;

/**
 * Orchestrates transfers around the transactional core in
 * {@link TransferExecutionService}. Outside-transaction responsibilities:
 * client authentication, idempotency conflict resolution, and FAILED-state
 * recording in a fresh transaction.
 */
@Service
public class TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    /**
     * Defaults exposed as constants so tests can construct the service without
     * fishing literals out of the {@code @Value} placeholder strings. These
     * three places — the constants here, the {@code @Value} fallbacks in the
     * constructor, and the {@code transfer.retry.*} entries in
     * {@code application.yml} — must be kept in sync by hand; Spring offers no
     * single-source-of-truth wiring for primitive {@code @Value} defaults.
     * A future move to {@code @ConfigurationProperties} would let one record
     * own all three.
     */
    static final int DEFAULT_MAX_EXECUTION_ATTEMPTS = 3;
    static final long DEFAULT_BASE_BACKOFF_MILLIS = 10L;
    static final long DEFAULT_JITTER_MILLIS = 10L;
    static final int DEFAULT_TRANSIENT_CONFLICT_RETRY_AFTER_SECONDS = 1;

    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final TransferExecutionService transferExecutionService;
    private final IdempotencyFailureRecorder failureRecorder;
    private final ClientRepository clientRepository;
    private final TransferMetrics metrics;

    /**
     * Caps the conflict-then-rollback retry budget. Wired as field-level
     * config rather than a static constant so an oncall can dial it via
     * {@code transfer.retry.max-attempts} during a hot-shard incident
     * without a redeploy — a static constant was considered but would force
     * a redeploy on every tuning change, which is exactly the wrong shape
     * for an incident-response knob. A bound {@code < 1} would short-circuit
     * the loop and always emit 503; the constructor rejects that case loudly.
     */
    private final int maxExecutionAttempts;
    /** Base back-off applied between retry attempts (milliseconds). */
    private final long baseBackoffMillis;
    /**
     * Random jitter on top of {@link #baseBackoffMillis} to avoid lock-step
     * retries. {@code 0} means deterministic backoff — explicitly handled in
     * {@link #backoffWithJitter} so {@link ThreadLocalRandom#nextLong(long)}
     * never sees a zero bound (which throws).
     */
    private final long jitterMillis;
    /**
     * Hint to clients on the 503 surfaced when retries exhaust. Keep small —
     * the server has already exhausted its retry budget; the client just
     * needs to step out of the hot window before the next attempt.
     */
    private final int transientConflictRetryAfterSeconds;

    public TransferService(IdempotencyRecordRepository idempotencyRecordRepository,
                           TransferExecutionService transferExecutionService,
                           IdempotencyFailureRecorder failureRecorder,
                           ClientRepository clientRepository,
                           TransferMetrics metrics,
                           @Value("${transfer.retry.max-attempts:3}") int maxExecutionAttempts,
                           @Value("${transfer.retry.base-backoff-millis:10}") long baseBackoffMillis,
                           @Value("${transfer.retry.jitter-millis:10}") long jitterMillis,
                           @Value("${transfer.retry.retry-after-seconds:1}") int transientConflictRetryAfterSeconds) {
        // Fail fast on misconfiguration: a negative or zero retry budget would
        // skip the loop entirely and surface every conflict as a 503, hiding
        // legitimate IdempotencyConflictException paths from clients.
        if (maxExecutionAttempts < 1) {
            throw new IllegalArgumentException(
                    "transfer.retry.max-attempts must be >= 1 (was " + maxExecutionAttempts + ")");
        }
        if (baseBackoffMillis < 0) {
            throw new IllegalArgumentException(
                    "transfer.retry.base-backoff-millis must be >= 0 (was " + baseBackoffMillis + ")");
        }
        // ThreadLocalRandom.nextLong(bound) requires bound > 0; we treat 0 as
        // "no jitter" in backoffWithJitter, but anything negative is a config
        // error and we surface it here rather than at the first retry.
        if (jitterMillis < 0) {
            throw new IllegalArgumentException(
                    "transfer.retry.jitter-millis must be >= 0 (was " + jitterMillis + ")");
        }
        if (transientConflictRetryAfterSeconds < 0) {
            throw new IllegalArgumentException(
                    "transfer.retry.retry-after-seconds must be >= 0 (was "
                            + transientConflictRetryAfterSeconds + ")");
        }
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.transferExecutionService = transferExecutionService;
        this.failureRecorder = failureRecorder;
        this.clientRepository = clientRepository;
        this.metrics = metrics;
        this.maxExecutionAttempts = maxExecutionAttempts;
        this.baseBackoffMillis = baseBackoffMillis;
        this.jitterMillis = jitterMillis;
        this.transientConflictRetryAfterSeconds = transientConflictRetryAfterSeconds;
    }

    public TransferResult executeTransfer(String clientId, String idempotencyKey, CreateTransferRequest request) {
        // Latency timer wraps the entire method — including the unknown-client
        // reject path — so the histogram reflects what the caller actually
        // experiences. Outcome tag is updated at every terminal point and
        // emitted in the finally block; if a wholly unexpected exception
        // escapes, the default OUTCOME_ERROR ensures the sample still lands
        // somewhere queryable rather than vanishing.
        Timer.Sample sample = metrics.startExecutionTimer();
        String outcome = TransferMetrics.OUTCOME_ERROR;
        try {
            // Reject unknown clients up front for a clean 403 instead of an FK violation.
            if (!clientRepository.existsById(clientId)) {
                log.warn("Rejecting transfer request from unknown client: {}", clientId);
                outcome = TransferMetrics.OUTCOME_UNKNOWN_CLIENT;
                throw new UnknownClientException(clientId);
            }

            String requestHash = Hashing.sha256Hex(request.toCanonicalString());

            for (int attempt = 1; attempt <= maxExecutionAttempts; attempt++) {
                try {
                    TransferResult fresh = transferExecutionService.tryExecuteTransfer(
                            clientId, idempotencyKey, request, requestHash);
                    outcome = TransferMetrics.OUTCOME_SUCCESS;
                    return fresh;
                } catch (IdempotencyConflictException e) {
                    // Key claimed by another transaction. resolveConflict returns null
                    // when the prior transaction rolled back — loop and retry.
                    TransferResult resolved = resolveConflict(clientId, idempotencyKey, requestHash, attempt);
                    if (resolved != null) {
                        outcome = TransferMetrics.OUTCOME_REPLAY;
                        return resolved;
                    }
                    // Brief jittered back-off before the next attempt — without it
                    // a deterministic rollback (e.g. wallet missing) lets all three
                    // retries complete in microseconds and gives no peer transaction
                    // a chance to commit. Skip the sleep on the final iteration; a
                    // back-off after the last attempt only adds latency to the 503.
                    if (attempt < maxExecutionAttempts) {
                        backoffWithJitter(idempotencyKey);
                    }
                } catch (WalletNotFoundException e) {
                    // The recorder owns both the FAILED row write and the per-reason
                    // failure metric, the latter registered as afterCommit so the
                    // counter only moves once the row is durable.
                    failureRecorder.recordWalletNotFoundFailure(clientId, idempotencyKey, requestHash, e.getMessage());
                    outcome = TransferMetrics.OUTCOME_WALLET_NOT_FOUND;
                    throw e;
                } catch (InsufficientBalanceException e) {
                    failureRecorder.recordInsufficientBalanceFailure(
                            clientId, idempotencyKey, requestHash, request, e.getCurrentBalance());
                    outcome = TransferMetrics.OUTCOME_INSUFFICIENT_BALANCE;
                    throw e;
                }
            }
            // Distinguish "client mismatch" (genuine 409 from resolveConflict) from
            // "system thrashing" (this branch). 503 + Retry-After is the standard
            // transient-failure signal — a 409 here would mislead anyone reading
            // a hot-shard incident dashboard.
            log.error("Exceeded max execution attempts for transfer: client={}, key={}",
                    clientId, LogSafe.mask(idempotencyKey));
            outcome = TransferMetrics.OUTCOME_TRANSIENT_CONFLICT;
            throw new TransientIdempotencyConflictException(idempotencyKey, transientConflictRetryAfterSeconds);
        } catch (IdempotencyConflictException e) {
            // Reaches the outer catch only when resolveConflict re-throws on
            // hash mismatch — Java does not route an exception thrown inside
            // a catch block back into a sibling catch of the same try.
            outcome = TransferMetrics.OUTCOME_HASH_CONFLICT;
            throw e;
        } catch (IdempotencyKeyInProgressException e) {
            // Same path: thrown by resolveConflict on IN_PROGRESS records.
            outcome = TransferMetrics.OUTCOME_IN_PROGRESS;
            throw e;
        } catch (TransientIdempotencyConflictException e) {
            // Covers both the explicit-throw above (already tagged, re-tag is
            // idempotent) and the interrupt path inside backoffWithJitter,
            // which would otherwise escape with the default OUTCOME_ERROR.
            // Reason on the wire body keeps the deploy/contention distinction;
            // the outcome tag deliberately collapses both into one bucket so
            // 503 latency is queryable without picking between two series.
            outcome = TransferMetrics.OUTCOME_TRANSIENT_CONFLICT;
            throw e;
        } finally {
            metrics.recordExecutionDuration(sample, outcome);
        }
    }

    /**
     * Sleeps {@link #baseBackoffMillis} plus up to {@link #jitterMillis} of
     * uniform jitter. On interrupt, restores the flag and short-circuits to
     * the transient-conflict signal — the caller has indicated they no longer
     * want to wait, so escalating to 503 is the right semantic (vs. silently
     * swallowing the interrupt and retrying).
     *
     * <p>The {@link TransientIdempotencyConflictException.Reason#REQUEST_INTERRUPTED}
     * tag carries the distinct cause through to the wire body and the ops log:
     * the retry budget was <em>not</em> actually exhausted here — the request
     * thread was interrupted (typical: servlet container graceful-shutdown).
     * Without the tag, a 503 emitted during a deploy reads as "system was hot",
     * misdirecting incident response.
     */
    private void backoffWithJitter(String idempotencyKey) {
        // jitterMillis == 0 is "deterministic backoff" — explicit branch
        // because ThreadLocalRandom.nextLong(0) throws IllegalArgumentException.
        long sleepMs = baseBackoffMillis;
        if (jitterMillis > 0) {
            sleepMs += ThreadLocalRandom.current().nextLong(jitterMillis);
        }
        try {
            Thread.sleep(sleepMs);
        } catch (InterruptedException ie) {
            Thread.currentThread().interrupt();
            throw new TransientIdempotencyConflictException(
                    idempotencyKey,
                    transientConflictRetryAfterSeconds,
                    TransientIdempotencyConflictException.Reason.REQUEST_INTERRUPTED);
        }
    }

    /**
     * Returns a cached result on COMPLETED/FAILED, throws on hash mismatch
     * or IN_PROGRESS, or returns {@code null} when the prior transaction
     * rolled back (caller retries).
     */
    private TransferResult resolveConflict(String clientId, String key, String requestHash, int attempt) {
        IdempotencyRecord record = idempotencyRecordRepository.findById(clientId, key).orElse(null);

        if (record == null) {
            log.info("idempotency record rolled back, retrying transfer execution (attempt {}/{}): client={}, key={}",
                    attempt, maxExecutionAttempts, clientId, LogSafe.mask(key));
            return null;
        }

        if (!record.getRequestHash().equals(requestHash)) {
            log.warn("Idempotency conflict: client={}, key={} used with different payload",
                    clientId, LogSafe.mask(key));
            throw new IdempotencyConflictException(key);
        }

        idempotencyRecordRepository.touchReplay(clientId, key);

        switch (record.getStatus()) {
            case COMPLETED:
            case FAILED:
                log.info("returning cached response for duplicate request: client={}, key={}, status={}",
                        clientId, LogSafe.mask(key), record.getStatus());
                // Pass the cached body through verbatim. The row was written by
                // ObjectMapper.writeValueAsString in TransferExecutionService /
                // IdempotencyFailureRecorder, so it's already valid JSON; parsing
                // it here only to let Jackson re-serialise it in the controller
                // would burn CPU on every duplicate request for no semantic gain.
                metrics.recordIdempotencyReplay();
                HttpStatus originalStatus = HttpStatus.valueOf(record.getResponseStatus());
                HttpStatus responseStatus = (originalStatus == HttpStatus.CREATED) ? HttpStatus.OK : originalStatus;
                return new TransferResult.Replay(record.getResponseBody(), responseStatus);

            case IN_PROGRESS:
                // Defensive arm — unreachable in the current wiring but
                // deliberately retained. INSERT ... ON CONFLICT DO NOTHING
                // blocks until the conflicting transaction commits or rolls
                // back, so by the time resolveConflict runs the peer has
                // already finalized the row to COMPLETED, FAILED, or back
                // to absent (rollback). The truly in-flight case is caught
                // upstream by the advisory-lock fast-path in
                // TransferExecutionService.guardConcurrentRequest.
                //
                // Collapsing this branch into the default IllegalStateException
                // arm was considered as dead-code removal, and rejected: a
                // future refactor that splits the claim and execute into
                // separate transactions would silently surface an
                // IN_PROGRESS row to a peer, and the default arm would map
                // to a generic 5xx instead of the right wire response. A
                // maintainer who deletes this branch should first verify
                // the new wiring cannot surface IN_PROGRESS to a peer.
                //
                // If the case ever does fire, 409 (via IdempotencyKeyInProgressException)
                // is the right wire response: the same key is in flight in another
                // transaction, the client should back off and retry — exactly what
                // the advisory-lock fast-path returns today, so the semantic on the
                // wire stays consistent if this branch ever activates.
                log.info("duplicate request detected while in-progress: client={}, key={}",
                        clientId, LogSafe.mask(key));
                throw new IdempotencyKeyInProgressException(key);

            default:
                throw new IllegalStateException("Unexpected idempotency status: " + record.getStatus());
        }
    }

    /**
     * Result of an {@code executeTransfer} call. Two shapes, statically
     * disjoint via a sealed hierarchy so the wrong branch is a compile error
     * instead of a 5xx in production:
     * <ul>
     *   <li>{@link Fresh} — a live DTO produced by the just-executed transfer.
     *       The framework Jackson-serialises it on the wire.</li>
     *   <li>{@link Replay} — the cached response body from
     *       {@code idempotency_records.response_body}, which was already JSON
     *       when written. Shipping it as raw bytes avoids the
     *       parse-then-reserialise round trip an {@code Object.class} read in
     *       {@link #resolveConflict} would otherwise force on every duplicate
     *       request.</li>
     * </ul>
     *
     * <p>{@link com.wallet.controller.TransferController} pattern-switches on
     * the variant to choose between Jackson serialisation and a raw-bytes
     * write. The compiler enforces exhaustive coverage; adding a third
     * variant fails the controller fast at build time, where it can be fixed,
     * rather than at request time, where it cannot.
     */
    public sealed interface TransferResult permits TransferResult.Fresh, TransferResult.Replay {

        /** HTTP status to write on the wire — common to both shapes. */
        HttpStatus httpStatus();

        /** Fresh-execution shape — the framework serialises {@code response}. */
        record Fresh(Object response, HttpStatus httpStatus) implements TransferResult {}

        /** Replay shape — {@code rawJson} is shipped verbatim with {@code application/json}. */
        record Replay(String rawJson, HttpStatus httpStatus) implements TransferResult {}
    }
}
