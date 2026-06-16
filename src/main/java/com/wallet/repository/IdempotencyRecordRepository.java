package com.wallet.repository;

import com.wallet.domain.IdempotencyRecord;
import com.wallet.domain.IdempotencyStatus;
import org.jooq.DSLContext;
import org.jooq.Record;
import org.jooq.impl.DSL;
import org.springframework.stereotype.Repository;

import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.Optional;
import java.util.UUID;

import static com.wallet.jooq.Tables.IDEMPOTENCY_RECORDS;

/**
 * Persistence for {@link IdempotencyRecord}. Methods are split by intent:
 * {@code insert*} claim, {@code mark*} transition, {@code touchReplay} bumps
 * the replay counter. No method silently mixes insert and update semantics.
 */
@Repository
public class IdempotencyRecordRepository {

    private final DSLContext dsl;

    public IdempotencyRecordRepository(DSLContext dsl) {
        this.dsl = dsl;
    }

    /**
     * Transaction-scoped advisory lock; auto-released on commit/rollback.
     *
     * <p>Deliberately the only flavour exposed. A session-scoped
     * {@code pg_try_advisory_lock(...)} paired with an explicit unlock was
     * considered and rejected: under HikariCP the acquire and the unlock can
     * land on different physical connections (each {@code dsl.*} call against
     * an auto-commit pool returns a fresh connection), so the unlock would be
     * a no-op against a connection that does not hold the lock and the lock
     * would stay pinned to its session until {@code max-lifetime} (30 min)
     * cycled it. If you need leader election, bind acquire and work to a
     * single transaction (see {@link com.wallet.service.IdempotencyCleanupService})
     * — do not introduce a session-scoped flavour here.
     */
    public boolean tryAdvisoryXactLock(long lockId) {
        return dsl.select(DSL.field("pg_try_advisory_xact_lock({0})", Boolean.class, lockId))
                .fetchOneInto(Boolean.class);
    }

    /** Returns 1 on insert, 0 on conflict — caller handles the duplicate case. */
    public int insertIdempotencyRecord(String clientId, String key, String hash, String status) {
        OffsetDateTime now = OffsetDateTime.now(ZoneOffset.UTC);
        return dsl.insertInto(IDEMPOTENCY_RECORDS)
                .set(IDEMPOTENCY_RECORDS.CLIENT_ID, clientId)
                .set(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY, key)
                .set(IDEMPOTENCY_RECORDS.REQUEST_HASH, hash)
                .set(IDEMPOTENCY_RECORDS.STATUS, status)
                .set(IDEMPOTENCY_RECORDS.TIMES_SEEN, 1)
                .set(IDEMPOTENCY_RECORDS.FIRST_SEEN_AT, now)
                .set(IDEMPOTENCY_RECORDS.LAST_SEEN_AT, now)
                .onConflict(IDEMPOTENCY_RECORDS.CLIENT_ID, IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY)
                .doNothing()
                .execute();
    }

    /**
     * Inserts a FAILED record directly. Used after the executing transaction
     * has rolled back its IN_PROGRESS row, so a fresh INSERT normally succeeds.
     * Returns 0 on conflict — the caller decides whether to overwrite.
     */
    public int insertFailedRecord(String clientId, String key, String requestHash,
                                  UUID transferId, int responseStatus, String responseBody) {
        OffsetDateTime now = OffsetDateTime.now(ZoneOffset.UTC);
        return dsl.insertInto(IDEMPOTENCY_RECORDS)
                .set(IDEMPOTENCY_RECORDS.CLIENT_ID, clientId)
                .set(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY, key)
                .set(IDEMPOTENCY_RECORDS.REQUEST_HASH, requestHash)
                .set(IDEMPOTENCY_RECORDS.STATUS, IdempotencyStatus.FAILED.name())
                .set(IDEMPOTENCY_RECORDS.TRANSFER_ID, transferId)
                .set(IDEMPOTENCY_RECORDS.RESPONSE_STATUS, responseStatus)
                .set(IDEMPOTENCY_RECORDS.RESPONSE_BODY, responseBody)
                .set(IDEMPOTENCY_RECORDS.TIMES_SEEN, 1)
                .set(IDEMPOTENCY_RECORDS.FIRST_SEEN_AT, now)
                .set(IDEMPOTENCY_RECORDS.LAST_SEEN_AT, now)
                .onConflict(IDEMPOTENCY_RECORDS.CLIENT_ID, IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY)
                .doNothing()
                .execute();
    }

    public Optional<IdempotencyRecord> findById(String clientId, String key) {
        return dsl.selectFrom(IDEMPOTENCY_RECORDS)
                .where(IDEMPOTENCY_RECORDS.CLIENT_ID.eq(clientId))
                .and(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY.eq(key))
                .fetchOptional()
                .map(this::mapRecordToIdempotencyRecord);
    }

    /** Transitions IN_PROGRESS → COMPLETED. Returns 1 if matched, 0 otherwise. */
    public int markCompleted(String clientId, String key, UUID transferId, int responseStatus, String responseBody) {
        OffsetDateTime now = OffsetDateTime.now(ZoneOffset.UTC);
        return dsl.update(IDEMPOTENCY_RECORDS)
                .set(IDEMPOTENCY_RECORDS.STATUS, IdempotencyStatus.COMPLETED.name())
                .set(IDEMPOTENCY_RECORDS.TRANSFER_ID, transferId)
                .set(IDEMPOTENCY_RECORDS.RESPONSE_STATUS, responseStatus)
                .set(IDEMPOTENCY_RECORDS.RESPONSE_BODY, responseBody)
                .set(IDEMPOTENCY_RECORDS.LAST_SEEN_AT, now)
                .where(IDEMPOTENCY_RECORDS.CLIENT_ID.eq(clientId))
                .and(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY.eq(key))
                .execute();
    }

    // Note: there is intentionally no markFailed(...) method.
    //
    // A symmetric markFailed UPDATE was considered alongside markCompleted
    // and rejected: by the time the failure path runs, the executing
    // transaction has already rolled back its IN_PROGRESS row, so there is
    // no row left for an UPDATE to transition. The statement would no-op
    // against the absent row and the failure record would silently never
    // persist. The recorder instead opens a fresh REQUIRES_NEW transaction
    // in IdempotencyFailureRecorder and INSERTs a FAILED record via
    // insertFailedRecord (idempotent on (client_id, key)) — the only shape
    // that survives the rolled-back IN_PROGRESS state. Omitting the UPDATE
    // method keeps that single durable shape the only available primitive.

    /**
     * Increments {@code times_seen} and updates {@code last_seen_at}. No other
     * column is touched — a replay never mutates the cached response.
     *
     * <p>Concurrency: the {@code SET times_seen = times_seen + 1} expression is
     * evaluated server-side. Under READ_COMMITTED, Postgres serializes the two
     * UPDATEs by taking a row lock; the second waiter re-reads the latest
     * committed value of {@code times_seen} before evaluating its SET clause,
     * so increments are linearized and never lost. The transitions
     * {@link #markCompleted} (and the FAILED-state INSERT in
     * {@link #insertFailedRecord}) cannot race with replays because they run
     * before the row is replayable, so cross-statement interference is also
     * impossible. {@code times_seen} is therefore exact for observability,
     * but is still not intended as a billing signal.
     */
    public int touchReplay(String clientId, String key) {
        OffsetDateTime now = OffsetDateTime.now(ZoneOffset.UTC);
        return dsl.update(IDEMPOTENCY_RECORDS)
                .set(IDEMPOTENCY_RECORDS.TIMES_SEEN, IDEMPOTENCY_RECORDS.TIMES_SEEN.plus(1))
                .set(IDEMPOTENCY_RECORDS.LAST_SEEN_AT, now)
                .where(IDEMPOTENCY_RECORDS.CLIENT_ID.eq(clientId))
                .and(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY.eq(key))
                .execute();
    }

    /**
     * Filters on {@code first_seen_at} — gives a hard retention ceiling so
     * frequently-replayed keys cannot extend their lifetime indefinitely.
     *
     * <p><strong>Bounded batch.</strong> Deletes at most {@code batchSize} rows
     * per call. Callers that want to drain a backlog should call repeatedly
     * (see {@link com.wallet.service.IdempotencyCleanupService}). A single
     * unbounded {@code DELETE WHERE first_seen_at < threshold} would, on a
     * large {@code idempotency_records} table, produce a long-running
     * transaction, hold row locks across the entire matched set, bloat WAL,
     * and trip the {@code statement_timeout = 10s} guard configured in
     * {@code application.yml} (SQLState 57014). The batched form caps each
     * statement at {@code batchSize} rows so it stays well below the timeout,
     * and the leader-election lock around the loop prevents two instances
     * from colliding on the same retention sweep.
     */
    public int deleteOlderThan(OffsetDateTime threshold, int batchSize) {
        if (batchSize < 1) {
            throw new IllegalArgumentException("batchSize must be >= 1 (was " + batchSize + ")");
        }
        return dsl.deleteFrom(IDEMPOTENCY_RECORDS)
                .where(DSL.row(IDEMPOTENCY_RECORDS.CLIENT_ID, IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY)
                        .in(dsl.select(IDEMPOTENCY_RECORDS.CLIENT_ID, IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY)
                                .from(IDEMPOTENCY_RECORDS)
                                .where(IDEMPOTENCY_RECORDS.FIRST_SEEN_AT.lt(threshold))
                                .limit(batchSize)))
                .execute();
    }

    private IdempotencyRecord mapRecordToIdempotencyRecord(Record r) {
        if (r == null) return null;
        OffsetDateTime firstSeenAt = r.get(IDEMPOTENCY_RECORDS.FIRST_SEEN_AT);
        OffsetDateTime lastSeenAt = r.get(IDEMPOTENCY_RECORDS.LAST_SEEN_AT);
        return new IdempotencyRecord(
                r.get(IDEMPOTENCY_RECORDS.CLIENT_ID),
                r.get(IDEMPOTENCY_RECORDS.IDEMPOTENCY_KEY),
                r.get(IDEMPOTENCY_RECORDS.REQUEST_HASH),
                r.get(IDEMPOTENCY_RECORDS.TRANSFER_ID),
                IdempotencyStatus.valueOf(r.get(IDEMPOTENCY_RECORDS.STATUS)),
                r.get(IDEMPOTENCY_RECORDS.RESPONSE_STATUS),
                r.get(IDEMPOTENCY_RECORDS.RESPONSE_BODY),
                r.get(IDEMPOTENCY_RECORDS.TIMES_SEEN),
                firstSeenAt != null ? firstSeenAt.toInstant() : null,
                lastSeenAt != null ? lastSeenAt.toInstant() : null
        );
    }
}
