package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.util.Hashing;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import javax.sql.DataSource;
import java.sql.Connection;
import java.sql.Statement;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Targeted integration test for the {@link com.wallet.exception.IdempotencyKeyInProgressException}
 * 409 contract. The advisory lock is held from a separate database session to
 * simulate an in-flight peer transaction; the request thread must reject with
 * 409 rather than block, succeed, or 5xx. Without this test the exception's
 * mapping is only exercised opportunistically by the racing concurrent-replay
 * suite, where the outcome can vary between 409 and 200/201.
 *
 * <p>Class-level {@code @SuppressWarnings} mirrors {@link TransferIntegrationTest}:
 * error envelopes are accepted as raw {@code Map}, with assertions explicitly
 * coercing values via {@code String.valueOf} / {@code Number} casts.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
class IdempotencyKeyInProgressIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Autowired
    private DataSource dataSource;

    @Test
    @DisplayName("Should return 409 when another session holds the advisory lock for the same idempotency key")
    void shouldReturn409WhenAdvisoryLockIsHeldByPeer() throws Exception {
        String clientId = "test-client";
        String idempotencyKey = "in-progress-" + UUID.randomUUID();

        // The lockId is derived deterministically from (clientId, key); the
        // application uses the same hash inside guardConcurrentRequest.
        long lockId = Hashing.toLockId(clientId + ":" + idempotencyKey);

        ExecutorService executor = Executors.newSingleThreadExecutor();
        CountDownLatch lockHeldLatch = new CountDownLatch(1);
        CountDownLatch releaseLatch = new CountDownLatch(1);

        // Hold pg_advisory_lock on a dedicated connection. The connection stays
        // open and the lock stays acquired until releaseLatch fires — that is
        // the precise window in which the API request must observe a 409.
        Future<Boolean> lockHolder = executor.submit(() -> {
            try (Connection conn = dataSource.getConnection();
                 Statement stmt = conn.createStatement()) {
                conn.setAutoCommit(true);
                stmt.execute("SELECT pg_advisory_lock(" + lockId + ")");
                lockHeldLatch.countDown();
                // Hold until the test thread releases us.
                if (!releaseLatch.await(15, TimeUnit.SECONDS)) {
                    return false;
                }
                stmt.execute("SELECT pg_advisory_unlock(" + lockId + ")");
                return true;
            }
        });

        try {
            // Wait until the lock is provably held before issuing the request.
            assertThat(lockHeldLatch.await(10, TimeUnit.SECONDS))
                    .as("peer session must acquire advisory lock first")
                    .isTrue();

            CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
            ResponseEntity<Map> response = postTransfer(
                    restTemplate, clientId, idempotencyKey, request, Map.class);

            // Contract: pg_try_advisory_xact_lock fails fast because the peer
            // holds the lock, so we get 409 — never 200, 201, or 5xx.
            assertThat(response.getStatusCode())
                    .as("must be 409 CONFLICT, not blocked or 5xx")
                    .isEqualTo(HttpStatus.CONFLICT);

            Map<?, ?> body = response.getBody();
            assertThat(body).isNotNull();
            assertThat(body.get("statusCode")).isEqualTo(HttpStatus.CONFLICT.value());
            // Pin to the exact message produced by IdempotencyKeyInProgressException.
            // A loose contains("in progress") would also accept any future
            // IdempotencyConflictException whose message happens to mention
            // "in progress" — both share the 409 status, so the message is the
            // only on-the-wire discriminator today. Equality protects against
            // that drift; if a structured errorCode is added later, prefer it.
            assertThat(String.valueOf(body.get("message")))
                    .isEqualTo("Idempotency key '" + idempotencyKey + "' is already in progress");

            // The aborted request must NOT have written an idempotency row —
            // claim happens only after the advisory lock is acquired.
            assertThat(idempotencyRecordRepository.findById(clientId, idempotencyKey)).isEmpty();
        } finally {
            releaseLatch.countDown();
            assertThat(lockHolder.get(15, TimeUnit.SECONDS)).isTrue();
            executor.shutdown();
        }
    }
}
