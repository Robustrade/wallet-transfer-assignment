package com.robustrade.wallet.metrics;

import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.DistributionSummary;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import java.math.BigDecimal;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.function.Supplier;
import org.springframework.stereotype.Component;

/**
 * Transfer-domain metrics: throughput, errors, latency, and outcome counters.
 *
 * <p>HTTP-level throughput / latency / errors also come from Actuator's
 * {@code http.server.requests} timer (exported on {@code /actuator/prometheus}).
 */
@Component
public class TransferMetrics {

    public static final String METRIC_REQUESTS = "wallet.transfer.requests";
    public static final String METRIC_ERRORS = "wallet.transfer.errors";
    public static final String METRIC_LATENCY = "wallet.transfer.latency";
    public static final String METRIC_OUTCOMES = "wallet.transfer.outcomes";
    public static final String METRIC_AMOUNT = "wallet.transfer.amount";
    public static final String METRIC_RETRIES = "wallet.transfer.retries";
    public static final String METRIC_INSUFFICIENT = "wallet.transfer.insufficient_balance";
    public static final String METRIC_IDEMPOTENCY_CONFLICT = "wallet.transfer.idempotency_conflicts";
    public static final String METRIC_UNIQUE_KEY_RACE = "wallet.transfer.unique_key_races";

    private final MeterRegistry registry;
    private final Counter requests;
    private final Counter retries;
    private final Counter insufficientBalance;
    private final Counter idempotencyConflicts;
    private final Counter uniqueKeyRaces;
    private final DistributionSummary amountSummary;
    private final ConcurrentMap<String, Counter> outcomeCounters = new ConcurrentHashMap<>();
    private final ConcurrentMap<String, Counter> errorCounters = new ConcurrentHashMap<>();
    private final ConcurrentMap<String, Timer> latencyTimers = new ConcurrentHashMap<>();

    public TransferMetrics(MeterRegistry registry) {
        this.registry = registry;
        this.requests = Counter.builder(METRIC_REQUESTS)
                .description("Transfer processing attempts (Prometheus throughput: rate(..._total[1m]))")
                .register(registry);
        this.retries = Counter.builder(METRIC_RETRIES)
                .description("Failed transfers that entered RETRY flow")
                .register(registry);
        this.insufficientBalance = Counter.builder(METRIC_INSUFFICIENT)
                .description("Transfers rejected for insufficient balance")
                .register(registry);
        this.idempotencyConflicts = Counter.builder(METRIC_IDEMPOTENCY_CONFLICT)
                .description("Idempotency key reused with a different payload")
                .register(registry);
        this.uniqueKeyRaces = Counter.builder(METRIC_UNIQUE_KEY_RACE)
                .description("Concurrent inserts racing on idempotency_key unique constraint")
                .register(registry);
        this.amountSummary = DistributionSummary.builder(METRIC_AMOUNT)
                .description("Transfer amounts for newly completed PROCESSED transfers")
                .baseUnit("currency")
                .publishPercentiles(0.5, 0.95, 0.99)
                .register(registry);
    }

    /**
     * Times a transfer request and records throughput / outcome / error metrics.
     */
    public <T> T track(Supplier<T> action) {
        requests.increment();
        Timer.Sample sample = Timer.start(registry);
        TransferMetricOutcome outcome = TransferMetricOutcome.UNEXPECTED;
        BigDecimal amount = null;
        boolean completedOk = false;
        try {
            TrackingContext.set(new TrackingContext());
            T result = action.get();
            TrackingContext context = TrackingContext.get();
            outcome = context.outcome != null ? context.outcome : TransferMetricOutcome.NEW_PROCESSED;
            amount = context.amount;
            completedOk = true;
            return result;
        } catch (RuntimeException ex) {
            TrackingContext context = TrackingContext.get();
            if (context != null && context.outcome != null) {
                outcome = context.outcome;
            } else {
                outcome = mapException(ex);
            }
            throw ex;
        } finally {
            TrackingContext.clear();
            String outcomeTag = outcome.name();
            sample.stop(latencyTimer(outcomeTag));
            outcomeCounter(outcomeTag).increment();
            if (!completedOk || isErrorOutcome(outcome)) {
                errorCounter(outcomeTag).increment();
            }
            if (outcome == TransferMetricOutcome.INSUFFICIENT_BALANCE) {
                insufficientBalance.increment();
            } else if (outcome == TransferMetricOutcome.IDEMPOTENCY_CONFLICT) {
                idempotencyConflicts.increment();
            }
            if (amount != null
                    && (outcome == TransferMetricOutcome.NEW_PROCESSED
                    || outcome == TransferMetricOutcome.RETRY_PROCESSED
                    || outcome == TransferMetricOutcome.RESUME_PROCESSED)) {
                amountSummary.record(amount.doubleValue());
            }
        }
    }

    public void markOutcome(TransferMetricOutcome outcome) {
        TrackingContext context = TrackingContext.get();
        if (context != null) {
            context.outcome = outcome;
        }
    }

    public void markOutcome(TransferMetricOutcome outcome, BigDecimal amount) {
        TrackingContext context = TrackingContext.get();
        if (context != null) {
            context.outcome = outcome;
            context.amount = amount;
        }
    }

    public void markRetry() {
        retries.increment();
    }

    public void markUniqueKeyRace() {
        uniqueKeyRaces.increment();
    }

    private static TransferMetricOutcome mapException(RuntimeException ex) {
        if (ex instanceof com.robustrade.wallet.service.TransferServiceException serviceException) {
            return switch (serviceException.getErrorCode()) {
                case "INSUFFICIENT_BALANCE" -> TransferMetricOutcome.INSUFFICIENT_BALANCE;
                case "IDEMPOTENCY_CONFLICT" -> TransferMetricOutcome.IDEMPOTENCY_CONFLICT;
                case "WALLETS_NOT_FOUND" -> TransferMetricOutcome.WALLETS_NOT_FOUND;
                case "TRANSFER_FAILED" -> TransferMetricOutcome.TRANSFER_FAILED;
                default -> TransferMetricOutcome.UNEXPECTED;
            };
        }
        String message = ex.getMessage();
        if (message != null
                && (message.contains("transfers_idempotency_key")
                || message.contains("Unique index or primary key violation"))) {
            return TransferMetricOutcome.UNEXPECTED;
        }
        return TransferMetricOutcome.UNEXPECTED;
    }

    private static boolean isErrorOutcome(TransferMetricOutcome outcome) {
        return outcome != TransferMetricOutcome.NEW_PROCESSED
                && outcome != TransferMetricOutcome.REPLAY_PROCESSED
                && outcome != TransferMetricOutcome.RETRY_PROCESSED
                && outcome != TransferMetricOutcome.RESUME_PROCESSED;
    }

    private Counter outcomeCounter(String outcome) {
        return outcomeCounters.computeIfAbsent(
                outcome,
                key -> Counter.builder(METRIC_OUTCOMES)
                        .description("Transfer processing outcomes")
                        .tag("outcome", key)
                        .register(registry)
        );
    }

    private Counter errorCounter(String errorCode) {
        return errorCounters.computeIfAbsent(
                errorCode,
                key -> Counter.builder(METRIC_ERRORS)
                        .description("Transfer processing errors by code")
                        .tag("error_code", key)
                        .register(registry)
        );
    }

    private Timer latencyTimer(String outcome) {
        return latencyTimers.computeIfAbsent(
                outcome,
                key -> Timer.builder(METRIC_LATENCY)
                        .description("Transfer processing latency")
                        .tag("outcome", key)
                        .publishPercentiles(0.5, 0.95, 0.99)
                        .register(registry)
        );
    }

    private static final class TrackingContext {
        private static final ThreadLocal<TrackingContext> HOLDER = new ThreadLocal<>();

        private TransferMetricOutcome outcome;
        private BigDecimal amount;

        static void set(TrackingContext context) {
            HOLDER.set(context);
        }

        static TrackingContext get() {
            return HOLDER.get();
        }

        static void clear() {
            HOLDER.remove();
        }
    }
}
