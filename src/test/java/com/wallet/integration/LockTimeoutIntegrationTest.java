package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.domain.IdempotencyRecord;
import com.wallet.repository.IdempotencyRecordRepository;
import org.jooq.DSLContext;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.TransactionDefinition;
import org.springframework.transaction.TransactionStatus;
import org.springframework.transaction.support.DefaultTransactionDefinition;

import java.util.Map;
import java.util.Optional;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration test that exercises the {@code lock_timeout} path specifically
 * — distinct from {@link TransactionTimeoutIntegrationTest}, which uses
 * Spring's {@code @Transactional(timeout=N)} (translated by the JDBC layer
 * via {@code Statement.setQueryTimeout}) to trigger SQLState {@code 57014}
 * via {@code statement_timeout}.
 *
 * <p>Postgres assigns timeouts to different SQLState classes:
 * <ul>
 *   <li>{@code statement_timeout} → SQLState {@code 57014} ({@code query_canceled},
 *       class 57 {@code operator_intervention}). Spring's translator routes
 *       this through {@code DataAccessResourceFailureException}.</li>
 *   <li>{@code lock_timeout} → SQLState {@code 55P03} ({@code lock_not_available},
 *       class 55 {@code object_not_in_prerequisite_state}). Spring's PostgreSQL
 *       vendor codes ({@code sql-error-codes.xml}: {@code cannotAcquireLockCodes=55P03})
 *       route this to {@link org.springframework.dao.CannotAcquireLockException}
 *       — a different handler in {@link com.wallet.exception.GlobalExceptionHandler}.</li>
 * </ul>
 *
 * <p>From the client's perspective both are timeouts on a row-lock wait, both
 * are retry-safe, both must surface as 504. This test locks down the
 * {@code 55P03} path: regression risk is high because it's an asymmetric
 * exception type that's easy to miss when refactoring the timeout family.
 *
 * <p>Configuration:
 * <ul>
 *   <li>{@code lock_timeout=200ms} — small so the test finishes fast.</li>
 *   <li>{@code statement_timeout=30s} — much larger, so the lock-wait fires
 *       first (otherwise the test would race between the two and could
 *       sometimes exercise the wrong path).</li>
 *   <li>{@code transfer.transaction.timeout-seconds=30} — matches above; the
 *       framework timer must not preempt the DB-side {@code lock_timeout}.</li>
 * </ul>
 */
@SpringBootTest(
        properties = {
                "transfer.transaction.timeout-seconds=30",
                "spring.datasource.hikari.connection-init-sql=SET lock_timeout = '200ms'; SET statement_timeout = '30s'"
        },
        webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT
)
class LockTimeoutIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Autowired
    private PlatformTransactionManager transactionManager;

    @Autowired
    private DSLContext dsl;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Test
    @DisplayName("lock_timeout (SQLState 55P03 → CannotAcquireLockException) returns 504")
    void lockTimeoutReturns504() {
        DefaultTransactionDefinition def = new DefaultTransactionDefinition();
        def.setPropagationBehavior(TransactionDefinition.PROPAGATION_REQUIRES_NEW);
        TransactionStatus status = transactionManager.getTransaction(def);

        try {
            // Hold an exclusive row lock on wallet_1 from the test thread (Connection A).
            // This will block any concurrent SELECT ... FOR UPDATE on the same row.
            dsl.selectFrom(com.wallet.jooq.Tables.WALLETS)
                    .where(com.wallet.jooq.Tables.WALLETS.ID.eq("wallet_1"))
                    .forUpdate()
                    .fetchOptional();

            // Issue a transfer wallet_1 → wallet_2. The execution thread will
            // attempt SELECT ... FOR UPDATE on wallet_1, block on the lock,
            // and after lock_timeout=200ms Postgres aborts the wait with
            // SQLState 55P03. Spring translates this to
            // CannotAcquireLockException; GlobalExceptionHandler maps to 504.
            String idempotencyKey = "lock-timeout-key-" + UUID.randomUUID();
            CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

            long startTime = System.currentTimeMillis();
            @SuppressWarnings("unchecked")
            ResponseEntity<Map<String, Object>> response =
                    (ResponseEntity) postTransfer(restTemplate, idempotencyKey, request, Map.class);
            long duration = System.currentTimeMillis() - startTime;

            // Primary assertion: 504, not 500. A regression that drops the
            // CannotAcquireLockException handler would surface as 500 here.
            assertThat(response.getStatusCode())
                    .as("lock_timeout must surface as 504, not 500 — see GlobalExceptionHandler.handleCannotAcquireLock")
                    .isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
            assertThat(response.getBody()).isNotNull();
            assertThat(response.getBody().get("message").toString()).contains("timed out");

            // Floor: must wait at least the configured lock_timeout before
            // bailing. A "fail open" regression that returns 504 instantly
            // (e.g. by short-circuiting the FOR UPDATE) would slip past the
            // status-code check; this floor catches it.
            //
            // Ceiling: should fire well before statement_timeout. The
            // lock_timeout=200ms config plus normal scheduling and Spring
            // exception-translation overhead is comfortably under 5s; if a
            // future change accidentally raises lock_timeout to the default
            // 8s, the test would still pass on correctness but burn an extra
            // 7+ seconds per CI run.
            assertThat(duration)
                    .as("must wait at least lock_timeout before timing out, must not approach statement_timeout")
                    .isGreaterThanOrEqualTo(200L)
                    .isLessThan(5_000L);

            // The execution transaction rolled back, so the idempotency
            // claim row should not be present — a retry on the same key is
            // safe (the contract RETRY_CONTRACT.md §504 promises).
            Optional<IdempotencyRecord> record =
                    idempotencyRecordRepository.findById("test-client", idempotencyKey);
            assertThat(record)
                    .as("rolled-back transaction must leave no idempotency claim — retry must be safe")
                    .isEmpty();
        } finally {
            // Release the exclusive lock on wallet_1.
            transactionManager.rollback(status);
        }
    }
}
