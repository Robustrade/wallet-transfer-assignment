package com.wallet.service;

import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.util.Hashing;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.TransactionDefinition;
import org.springframework.transaction.support.TransactionTemplate;

import java.time.OffsetDateTime;
import java.time.ZoneOffset;

/**
 * Evicts idempotency records older than the retention window. Each batch runs
 * in its own short transaction guarded by a transaction-scoped advisory lock
 * — only one instance per cluster does the work each tick.
 *
 * <p><strong>Why xact-scoped, not session-scoped.</strong> A session-level
 * {@code pg_try_advisory_lock} paired with an explicit {@code finally}-block
 * unlock was considered and rejected: each {@code dsl.*} call against an
 * auto-commit HikariCP-managed connection can pick a different physical
 * connection from the pool, so the acquire and the unlock are not guaranteed
 * to land on the same session. The unlock would be a no-op against a
 * connection that does not hold the lock, leaving the lock pinned to its
 * session until {@code max-lifetime} (30 min) cycled it — and the hourly
 * cron would silently skip for up to 30 minutes after each run. Binding
 * acquire and work to a single transaction sidesteps the problem:
 * {@code pg_try_advisory_xact_lock} releases automatically on
 * commit/rollback, so there is no explicit release path that can target
 * the wrong connection. The leader-election semantics are identical from
 * the caller's point of view.
 *
 * <p><strong>Why {@link TransactionTemplate}, not {@code @Transactional}.</strong>
 * Putting {@code @Transactional(REQUIRES_NEW)} on {@code deleteOneBatch} and
 * calling it from the loop in {@link #cleanupExpiredRecords} was considered
 * and rejected: that is a self-invocation through {@code this}, and Spring's
 * proxy-based transactional advice does not intercept it, so no transaction
 * would actually open. {@code pg_try_advisory_xact_lock} would release as
 * soon as its implicit auto-commit transaction ended at end of statement,
 * and two cluster instances ticking back-to-back could both acquire (each
 * in their own one-statement transaction) and then both DELETE in parallel
 * — the exact dual-runner race the lock is meant to prevent.
 * {@code TransactionTemplate.execute(...)} opens the transaction at the
 * call site, which keeps the lock alive for the duration of the DELETE and
 * makes the boundary visible to a reader.
 *
 * <p><strong>Why batched.</strong> A single unbounded
 * {@code DELETE WHERE first_seen_at < threshold} on a large
 * {@code idempotency_records} table would produce a long-running transaction,
 * bloat WAL, and trip the {@code statement_timeout = 10s} guard. The batched
 * form caps each statement at {@code batchSize} rows; we loop within one
 * scheduled tick until the batch is short, then yield to the next tick.
 */
@Service
public class IdempotencyCleanupService {

    private static final Logger log = LoggerFactory.getLogger(IdempotencyCleanupService.class);

    /** Stable lock id; change the seed string to avoid collisions when sharing a DB across apps. */
    private static final long CLEANUP_LEADER_LOCK_ID = Hashing.toLockId("wallet:idempotency-cleanup-leader");

    private final IdempotencyRecordRepository repository;
    private final TransactionTemplate batchTransactionTemplate;
    private final long retentionHours;
    private final int batchSize;
    /**
     * Hard ceiling on batches per scheduled tick — bounds total work even if
     * the table is huge. At default {@code batchSize=1000} this caps each tick
     * at {@code 50 × 1000 = 50_000} rows, well inside the {@code statement_timeout}
     * budget. Configurable so an oncall can widen it temporarily during a
     * backlog drain ({@code 100M}-row backlog at default settings would
     * otherwise need {@code 100M / 50_000 = 2000} hourly ticks ≈ 83 days) —
     * any remainder beyond the ceiling is picked up on the next cron tick.
     */
    private final int maxBatchesPerTick;

    public IdempotencyCleanupService(IdempotencyRecordRepository repository,
                                     PlatformTransactionManager transactionManager,
                                     @Value("${idempotency.cleanup.retention-hours:72}") long retentionHours,
                                     @Value("${idempotency.cleanup.batch-size:1000}") int batchSize,
                                     @Value("${idempotency.cleanup.max-batches-per-tick:50}") int maxBatchesPerTick) {
        if (batchSize < 1) {
            throw new IllegalArgumentException(
                    "idempotency.cleanup.batch-size must be >= 1 (was " + batchSize + ")");
        }
        if (maxBatchesPerTick < 1) {
            throw new IllegalArgumentException(
                    "idempotency.cleanup.max-batches-per-tick must be >= 1 (was " + maxBatchesPerTick + ")");
        }
        this.repository = repository;
        // REQUIRES_NEW so each batch is its own short transaction, isolated
        // from any ambient transaction the scheduler may have opened (it
        // doesn't today, but the explicit propagation pins the contract).
        TransactionTemplate template = new TransactionTemplate(transactionManager);
        template.setPropagationBehavior(TransactionDefinition.PROPAGATION_REQUIRES_NEW);
        this.batchTransactionTemplate = template;
        this.retentionHours = retentionHours;
        this.batchSize = batchSize;
        this.maxBatchesPerTick = maxBatchesPerTick;
    }

    /**
     * Defaults to once an hour, on the hour. Each invocation processes up to
     * {@link #maxBatchesPerTick} batches; any remainder is picked up next tick.
     *
     * <p>Per-batch failures are logged and the loop {@code continue}s rather
     * than aborting the tick — a transient deadlock or lock-timeout on one
     * batch should not skip the remaining {@code (maxBatchesPerTick - n)}
     * healthy batches. The cron's natural back-pressure (next tick re-runs the
     * full loop) covers the case where the failure is persistent.
     */
    @Scheduled(cron = "${idempotency.cleanup.cron:0 0 * * * *}")
    public void cleanupExpiredRecords() {
        OffsetDateTime threshold = OffsetDateTime.now(ZoneOffset.UTC).minusHours(retentionHours);
        log.debug("Starting expired idempotency records cleanup with threshold={}", threshold);

        int totalDeleted = 0;
        for (int batch = 0; batch < maxBatchesPerTick; batch++) {
            int deletedInBatch;
            try {
                deletedInBatch = deleteOneBatch(threshold);
            } catch (Exception e) {
                log.error("Failed to execute background idempotency records cleanup batch (continuing with next batch)", e);
                continue;
            }
            if (deletedInBatch == 0) {
                break;
            }
            totalDeleted += deletedInBatch;
        }
        if (totalDeleted > 0) {
            log.info("Cleaned up {} expired idempotency records (older than {} hours)",
                    totalDeleted, retentionHours);
        }
    }

    /**
     * One short, leader-elected batch. The advisory lock and the DELETE share
     * a single transaction opened explicitly via {@link TransactionTemplate};
     * the lock releases automatically when that transaction commits or rolls
     * back, regardless of which physical connection HikariCP returns next.
     * Returns the number of rows deleted in this batch (0 if no leader, or no
     * rows older than the threshold remain).
     *
     * <p>Package-private (not {@code @Transactional}) so the test can call it
     * directly and the production caller goes through {@link TransactionTemplate}
     * — there is no proxy to bypass.
     */
    int deleteOneBatch(OffsetDateTime threshold) {
        Integer deleted = batchTransactionTemplate.execute(status -> {
            if (!repository.tryAdvisoryXactLock(CLEANUP_LEADER_LOCK_ID)) {
                log.debug("Skipping idempotency cleanup batch — another instance holds the leader lock");
                return 0;
            }
            return repository.deleteOlderThan(threshold, batchSize);
        });
        return deleted == null ? 0 : deleted;
    }

}
