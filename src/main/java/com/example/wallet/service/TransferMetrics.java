package com.example.wallet.service;

import com.example.wallet.model.TransferStatus;
import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.DistributionSummary;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import org.springframework.stereotype.Component;

import java.math.BigDecimal;

/**
 * Encapsulates all Micrometer metric operations for the transfer domain.
 *
 * Metrics exposed:
 *
 *   wallet.transfers.total        (counter)  tags: outcome=PROCESSED|FAILED|REPLAY|ERROR
 *   wallet.transfers.duration     (timer)    tags: outcome=PROCESSED|FAILED|REPLAY|ERROR
 *   wallet.transfers.amount       (summary)  tags: outcome=PROCESSED|FAILED   unit: currency units
 *   wallet.idempotency.replays    (counter)  tags: path=fast_path|in_tx|on_conflict
 *   wallet.cleanup.resolved       (counter)  stuck PENDING transfers marked FAILED by scheduler
 *
 * Automatic metrics from Spring Boot Actuator:
 *   http.server.requests          latency and status of every HTTP call
 *   hikaricp.*                    connection pool utilisation
 *   jvm.*                         heap, GC, threads
 *   system.*                      CPU, load average
 *
 * Scrape endpoint: GET http://localhost:9090/actuator/prometheus
 */
@Component
public class TransferMetrics {

    // ── Metric name constants ─────────────────────────────────────────────────
    private static final String TRANSFERS_TOTAL    = "wallet.transfers.total";
    private static final String TRANSFERS_DURATION = "wallet.transfers.duration";
    private static final String TRANSFERS_AMOUNT   = "wallet.transfers.amount";
    private static final String IDEMPOTENCY_REPLAY = "wallet.idempotency.replays";
    private static final String CLEANUP_RESOLVED   = "wallet.cleanup.resolved";

    private static final String TAG_OUTCOME = "outcome";
    private static final String TAG_PATH    = "path";

    private final MeterRegistry registry;

    public TransferMetrics(MeterRegistry registry) {
        this.registry = registry;

        // Pre-register counters so they appear in /actuator/prometheus with value 0
        // even before any transfers are processed — important for alerting on absence.
        preRegisterCounter(TRANSFERS_TOTAL, TAG_OUTCOME, "PROCESSED");
        preRegisterCounter(TRANSFERS_TOTAL, TAG_OUTCOME, "FAILED");
        preRegisterCounter(TRANSFERS_TOTAL, TAG_OUTCOME, "REPLAY");
        preRegisterCounter(TRANSFERS_TOTAL, TAG_OUTCOME, "ERROR");
        preRegisterCounter(IDEMPOTENCY_REPLAY, TAG_PATH, "fast_path");
        preRegisterCounter(IDEMPOTENCY_REPLAY, TAG_PATH, "in_tx");
        preRegisterCounter(IDEMPOTENCY_REPLAY, TAG_PATH, "on_conflict");
        preRegisterCounter(CLEANUP_RESOLVED);
    }

    // ── Timer management ─────────────────────────────────────────────────────

    /** Start a timer sample at the beginning of a transfer execution. */
    public Timer.Sample startTimer() {
        return Timer.start(registry);
    }

    // ── Outcome recording ─────────────────────────────────────────────────────

    /** Record a successfully processed new transfer. */
    public void recordProcessed(Timer.Sample sample, BigDecimal amount) {
        stopTimer(sample, "PROCESSED");
        registry.counter(TRANSFERS_TOTAL, TAG_OUTCOME, "PROCESSED").increment();
        amountSummary("PROCESSED").record(amount.doubleValue());
    }

    /** Record a new transfer that failed due to insufficient funds. */
    public void recordFailed(Timer.Sample sample, BigDecimal amount) {
        stopTimer(sample, "FAILED");
        registry.counter(TRANSFERS_TOTAL, TAG_OUTCOME, "FAILED").increment();
        amountSummary("FAILED").record(amount.doubleValue());
    }

    /** Record an idempotent replay — no new transfer was executed. */
    public void recordReplay(Timer.Sample sample, String path) {
        stopTimer(sample, "REPLAY");
        registry.counter(TRANSFERS_TOTAL, TAG_OUTCOME, "REPLAY").increment();
        registry.counter(IDEMPOTENCY_REPLAY, TAG_PATH, path).increment();
    }

    /** Record an unexpected error (wallet not found, invariant violation, etc.). */
    public void recordError(Timer.Sample sample) {
        stopTimer(sample, "ERROR");
        registry.counter(TRANSFERS_TOTAL, TAG_OUTCOME, "ERROR").increment();
    }

    /** Record a stuck PENDING transfer resolved by the cleanup scheduler. */
    public void recordCleanupResolved() {
        registry.counter(CLEANUP_RESOLVED).increment();
    }

    // ── Private helpers ───────────────────────────────────────────────────────

    private void stopTimer(Timer.Sample sample, String outcome) {
        sample.stop(Timer.builder(TRANSFERS_DURATION)
                .description("End-to-end duration of wallet transfer execution")
                .tag(TAG_OUTCOME, outcome)
                .register(registry));
    }

    private DistributionSummary amountSummary(String outcome) {
        return DistributionSummary.builder(TRANSFERS_AMOUNT)
                .description("Distribution of transfer amounts")
                .baseUnit("units")
                .tag(TAG_OUTCOME, outcome)
                .register(registry);
    }

    private void preRegisterCounter(String name, String tagKey, String tagValue) {
        Counter.builder(name).tag(tagKey, tagValue).register(registry);
    }

    private void preRegisterCounter(String name) {
        Counter.builder(name).register(registry);
    }
}

