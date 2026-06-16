package com.wallet.metrics;

import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import org.springframework.stereotype.Component;

import java.util.Map;

/**
 * Operational counters for transfers: successes, failures by reason, and
 * idempotency replays. Centralizing meter creation keeps tag conventions
 * consistent across call sites.
 */
@Component
public class TransferMetrics {

    private static final String FAILURE_REASON_TAG = "reason";
    private static final String EXECUTION_OUTCOME_TAG = "outcome";
    private static final String EXECUTION_DURATION_METRIC = "transfers.execute.duration";
    private static final String EXECUTION_DURATION_DESCRIPTION =
            "End-to-end POST /transfers latency, partitioned by outcome";

    /** Reason tag values for {@link #recordFailure(String)}. */
    public static final String REASON_WALLET_NOT_FOUND = "wallet_not_found";
    public static final String REASON_INSUFFICIENT_BALANCE = "insufficient_balance";

    /**
     * Outcome tag values for {@link #recordExecutionDuration(Timer.Sample, String)}.
     * Centralised so call sites cannot drift onto unrelated string forms ("ok"
     * vs "success") that would split a single tag series into two.
     */
    public static final String OUTCOME_SUCCESS = "success";
    public static final String OUTCOME_REPLAY = "replay";
    public static final String OUTCOME_UNKNOWN_CLIENT = "unknown_client";
    public static final String OUTCOME_WALLET_NOT_FOUND = "wallet_not_found";
    public static final String OUTCOME_INSUFFICIENT_BALANCE = "insufficient_balance";
    public static final String OUTCOME_HASH_CONFLICT = "hash_conflict";
    public static final String OUTCOME_IN_PROGRESS = "in_progress";
    public static final String OUTCOME_TRANSIENT_CONFLICT = "transient_conflict";
    public static final String OUTCOME_ERROR = "error";

    private final MeterRegistry registry;
    private final Counter created;
    private final Counter idempotencyReplays;
    private final Counter advisoryLockRejections;
    /**
     * Pre-registered execution timers, keyed by {@code OUTCOME_*}. Built once
     * at construction so the hot path is a {@link Map#get(Object)} rather than
     * a {@code Timer.builder(...).register(registry)} on every request — the
     * outcome set is finite and known here, so lazy per-call registration is
     * needless work.
     */
    private final Map<String, Timer> executionTimers;

    public TransferMetrics(MeterRegistry registry) {
        this.registry = registry;
        this.created = Counter.builder("transfers.created")
                .description("Number of wallet transfers that completed successfully")
                .register(registry);
        this.idempotencyReplays = Counter.builder("idempotency.replays.total")
                .description("Number of idempotent replays served from the cached response")
                .register(registry);
        this.advisoryLockRejections = Counter.builder("idempotency.advisory_lock.rejections")
                .description(
                        "Number of times pg_try_advisory_xact_lock returned false for an idempotency "
                                + "request — almost always a true concurrent retry, but a sustained "
                                + "non-zero rate at low traffic could also mean Hashing.toLockId hash "
                                + "collisions and is worth investigating")
                .register(registry);
        this.executionTimers = Map.ofEntries(
                Map.entry(OUTCOME_SUCCESS, buildExecutionTimer(OUTCOME_SUCCESS)),
                Map.entry(OUTCOME_REPLAY, buildExecutionTimer(OUTCOME_REPLAY)),
                Map.entry(OUTCOME_UNKNOWN_CLIENT, buildExecutionTimer(OUTCOME_UNKNOWN_CLIENT)),
                Map.entry(OUTCOME_WALLET_NOT_FOUND, buildExecutionTimer(OUTCOME_WALLET_NOT_FOUND)),
                Map.entry(OUTCOME_INSUFFICIENT_BALANCE, buildExecutionTimer(OUTCOME_INSUFFICIENT_BALANCE)),
                Map.entry(OUTCOME_HASH_CONFLICT, buildExecutionTimer(OUTCOME_HASH_CONFLICT)),
                Map.entry(OUTCOME_IN_PROGRESS, buildExecutionTimer(OUTCOME_IN_PROGRESS)),
                Map.entry(OUTCOME_TRANSIENT_CONFLICT, buildExecutionTimer(OUTCOME_TRANSIENT_CONFLICT)),
                Map.entry(OUTCOME_ERROR, buildExecutionTimer(OUTCOME_ERROR)));
    }

    /**
     * Builds (or returns the cached) timer for {@code outcome}.
     * Micrometer dedupes meters by ({@code name}, {@code tags}): repeated calls
     * with the same outcome return the same {@link Timer} instance, and the
     * {@link io.micrometer.core.instrument.Timer.Builder#publishPercentiles}
     * configuration is locked at first registration — subsequent builders with
     * different percentile config for the same key are silently ignored. So
     * even when the fallback in {@link #recordExecutionDuration} fires, the
     * cost is a {@code MeterRegistry} lookup, not a fresh percentile-histogram
     * allocation. Documented here to forestall future
     * "why is meter construction on the hot path" reviews.
     */
    private Timer buildExecutionTimer(String outcome) {
        return Timer.builder(EXECUTION_DURATION_METRIC)
                .description(EXECUTION_DURATION_DESCRIPTION)
                .tag(EXECUTION_OUTCOME_TAG, outcome)
                .publishPercentiles(0.5, 0.95, 0.99)
                .register(registry);
    }

    public void recordCreated() {
        created.increment();
    }

    /** Per-reason failure counter. Created lazily so unused reasons emit no series. */
    public void recordFailure(String reason) {
        Counter.builder("transfers.failed")
                .description("Number of wallet transfers that failed, partitioned by failure reason")
                .tag(FAILURE_REASON_TAG, reason)
                .register(registry)
                .increment();
    }

    public void recordIdempotencyReplay() {
        idempotencyReplays.increment();
    }

    public void recordAdvisoryLockRejection() {
        advisoryLockRejections.increment();
    }

    /** Starts a sample for the {@code transfers.execute.duration} timer. */
    public Timer.Sample startExecutionTimer() {
        return Timer.start(registry);
    }

    /**
     * Stops {@code sample} on the {@code transfers.execute.duration} timer
     * tagged with {@code outcome}. Timers are pre-registered for every
     * {@code OUTCOME_*} value, so the hot path is a map lookup. Unanticipated
     * outcomes fall back to lazy registration (defensive — should be treated
     * as a bug if it fires). Percentile rollups (p50/p95/p99) are published
     * in-process — Prometheus aggregates the histogram via the standard
     * {@code _bucket} export.
     */
    public void recordExecutionDuration(Timer.Sample sample, String outcome) {
        Timer timer = executionTimers.get(outcome);
        if (timer == null) {
            timer = buildExecutionTimer(outcome);
        }
        sample.stop(timer);
    }
}
