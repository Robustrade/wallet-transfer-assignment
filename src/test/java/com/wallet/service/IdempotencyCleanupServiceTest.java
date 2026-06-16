package com.wallet.service;

import com.wallet.repository.IdempotencyRecordRepository;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.TransactionStatus;
import org.springframework.transaction.support.SimpleTransactionStatus;
import org.springframework.transaction.support.TransactionSynchronizationManager;

import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.concurrent.atomic.AtomicBoolean;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the batched, xact-locked cleanup service. The service uses
 * {@code pg_try_advisory_xact_lock} (released automatically on transaction
 * commit/rollback) rather than session-scoped locks; there is therefore no
 * explicit {@code releaseAdvisoryLock} call to verify here — the absence of
 * such a call is the design.
 *
 * <p>The service opens its per-batch transaction via {@link org.springframework.transaction.support.TransactionTemplate}
 * (not {@code @Transactional}), because the loop in {@code cleanupExpiredRecords}
 * is a self-invocation that Spring's proxy advice does not intercept.
 * {@link #shouldOpenActiveTransactionAroundDeleteBatch} pins that contract: it
 * asserts {@link TransactionSynchronizationManager#isActualTransactionActive()}
 * returns {@code true} from inside the repository call. Without that assertion,
 * a future refactor that moved the work back to a self-invoked
 * {@code @Transactional} method would silently break leader election (no
 * transaction → xact lock auto-releases per statement → two instances can
 * race) and pass every other assertion in this class.
 */
class IdempotencyCleanupServiceTest {

    private static final long RETENTION_HOURS = 72;
    private static final int BATCH_SIZE = 1000;
    private static final int MAX_BATCHES_PER_TICK = 50;

    /**
     * Returns a {@link PlatformTransactionManager} mock that runs the callback
     * inside a "transaction" by binding {@link TransactionSynchronizationManager}
     * — enough for {@code isActualTransactionActive()} checks to flip true and
     * for {@code TransactionTemplate.execute} to commit/rollback against a real
     * status object.
     */
    private static PlatformTransactionManager fakeTxManager() {
        PlatformTransactionManager txManager = Mockito.mock(PlatformTransactionManager.class);
        when(txManager.getTransaction(any())).thenAnswer(inv -> {
            TransactionSynchronizationManager.setActualTransactionActive(true);
            TransactionSynchronizationManager.initSynchronization();
            return new SimpleTransactionStatus(true);
        });
        Mockito.doAnswer(inv -> {
            TransactionSynchronizationManager.clear();
            return null;
        }).when(txManager).commit(any(TransactionStatus.class));
        Mockito.doAnswer(inv -> {
            TransactionSynchronizationManager.clear();
            return null;
        }).when(txManager).rollback(any(TransactionStatus.class));
        return txManager;
    }

    @Test
    @DisplayName("Should delete records older than the configured retention window when leader lock is acquired")
    void shouldInvokeRepositoryDeleteOlderThan() {
        IdempotencyRecordRepository repository = Mockito.mock(IdempotencyRecordRepository.class);
        when(repository.tryAdvisoryXactLock(anyLong())).thenReturn(true);
        // First batch: 5 deleted; second batch: 0 → loop exits, exercising the
        // batched-loop semantics without burning the full MAX_BATCHES_PER_TICK budget.
        when(repository.deleteOlderThan(any(OffsetDateTime.class), anyInt()))
                .thenReturn(5)
                .thenReturn(0);

        IdempotencyCleanupService service = new IdempotencyCleanupService(
                repository, fakeTxManager(), RETENTION_HOURS, BATCH_SIZE, MAX_BATCHES_PER_TICK);

        service.cleanupExpiredRecords();

        // Two invocations expected: the productive batch (5 rows) and the empty
        // batch that signals "drained". Captor records the threshold from each.
        ArgumentCaptor<OffsetDateTime> captor = ArgumentCaptor.forClass(OffsetDateTime.class);
        verify(repository, times(2)).deleteOlderThan(captor.capture(), eq(BATCH_SIZE));

        OffsetDateTime threshold = captor.getValue();
        // Verify the threshold is roughly retention-hours ago (within a small tolerance for clock drift)
        assertThat(threshold).isBeforeOrEqualTo(OffsetDateTime.now(ZoneOffset.UTC).minusHours(RETENTION_HOURS));
        assertThat(threshold).isAfter(OffsetDateTime.now(ZoneOffset.UTC).minusHours(RETENTION_HOURS).minusSeconds(5));
    }

    @Test
    @DisplayName("Should skip deletion when another instance holds the leader lock")
    void shouldSkipWhenAnotherInstanceLeads() {
        IdempotencyRecordRepository repository = Mockito.mock(IdempotencyRecordRepository.class);
        when(repository.tryAdvisoryXactLock(anyLong())).thenReturn(false);

        IdempotencyCleanupService service = new IdempotencyCleanupService(
                repository, fakeTxManager(), RETENTION_HOURS, BATCH_SIZE, MAX_BATCHES_PER_TICK);

        service.cleanupExpiredRecords();

        // Lock not acquired → never reach the DELETE statement.
        verify(repository, never()).deleteOlderThan(any(OffsetDateTime.class), anyInt());
    }

    @Test
    @DisplayName("Should swallow repository failures and not propagate up the scheduler")
    void shouldNotPropagateOnFailure() {
        IdempotencyRecordRepository repository = Mockito.mock(IdempotencyRecordRepository.class);
        when(repository.tryAdvisoryXactLock(anyLong())).thenReturn(true);
        when(repository.deleteOlderThan(any(OffsetDateTime.class), anyInt()))
                .thenThrow(new RuntimeException("simulated DB failure"));

        IdempotencyCleanupService service = new IdempotencyCleanupService(
                repository, fakeTxManager(), RETENTION_HOURS, BATCH_SIZE, MAX_BATCHES_PER_TICK);

        // The service catches and logs internally; should not propagate so
        // that one bad tick does not poison the @Scheduled execution chain.
        service.cleanupExpiredRecords();
    }

    @Test
    @DisplayName("Should reject batchSize < 1 at construction")
    void shouldRejectInvalidBatchSize() {
        IdempotencyRecordRepository repository = Mockito.mock(IdempotencyRecordRepository.class);
        // Zero or negative batchSize would skip the DELETE entirely or produce
        // a Postgres error; surface it loudly at startup, not at the first tick.
        org.assertj.core.api.Assertions.assertThatThrownBy(
                () -> new IdempotencyCleanupService(repository, fakeTxManager(), RETENTION_HOURS, 0, MAX_BATCHES_PER_TICK))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("batch-size");
    }

    /**
     * Regression guard for the self-invocation pitfall. When this code lived
     * inside an {@code @Transactional(REQUIRES_NEW)} method called as
     * {@code this.deleteOneBatch(...)} from a sibling method on the same bean,
     * Spring's proxy advice did not run and no transaction was opened. The
     * advisory lock auto-released per statement, leaving leader election
     * silently broken. Asserting that
     * {@link TransactionSynchronizationManager#isActualTransactionActive()}
     * returns {@code true} at the moment the repository is touched would have
     * caught it; this test pins that contract.
     */
    @Test
    @DisplayName("Should run the per-batch work inside an active transaction (regression: proxy self-invocation pitfall)")
    void shouldOpenActiveTransactionAroundDeleteBatch() {
        IdempotencyRecordRepository repository = Mockito.mock(IdempotencyRecordRepository.class);
        AtomicBoolean txActiveDuringLock = new AtomicBoolean(false);
        AtomicBoolean txActiveDuringDelete = new AtomicBoolean(false);

        when(repository.tryAdvisoryXactLock(anyLong())).thenAnswer(inv -> {
            txActiveDuringLock.set(TransactionSynchronizationManager.isActualTransactionActive());
            return true;
        });
        when(repository.deleteOlderThan(any(OffsetDateTime.class), anyInt())).thenAnswer(inv -> {
            txActiveDuringDelete.set(TransactionSynchronizationManager.isActualTransactionActive());
            return 0;
        });

        IdempotencyCleanupService service = new IdempotencyCleanupService(
                repository, fakeTxManager(), RETENTION_HOURS, BATCH_SIZE, MAX_BATCHES_PER_TICK);

        service.cleanupExpiredRecords();

        verify(repository, atLeastOnce()).tryAdvisoryXactLock(anyLong());
        assertThat(txActiveDuringLock)
                .as("advisory lock acquisition must run inside an active transaction so xact-scoped locks survive past the statement")
                .isTrue();
        assertThat(txActiveDuringDelete)
                .as("the DELETE must run in the same transaction that holds the advisory lock")
                .isTrue();
    }
}
