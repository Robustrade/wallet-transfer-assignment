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
 * Integration test verifying the transaction timeout behavior.
 * Configures a short timeout of 1 second to verify that timed-out transactions
 * are cleanly cancelled and roll back the associated idempotency key.
 */
@SpringBootTest(
        properties = "transfer.transaction.timeout-seconds=1",
        webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT
)
class TransactionTimeoutIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Autowired
    private PlatformTransactionManager transactionManager;

    @Autowired
    private DSLContext dsl;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Test
    @DisplayName("Should timeout and cancel idempotency key under lock contention")
    void shouldTimeoutAndCancelIdempotencyKeyUnderLockContention() throws Exception {
        DefaultTransactionDefinition def = new DefaultTransactionDefinition();
        def.setPropagationBehavior(TransactionDefinition.PROPAGATION_REQUIRES_NEW);
        TransactionStatus status = transactionManager.getTransaction(def);

        try {
            // Lock wallet_1 from the test thread (Connection A)
            dsl.selectFrom(com.wallet.jooq.Tables.WALLETS)
                    .where(com.wallet.jooq.Tables.WALLETS.ID.eq("wallet_1"))
                    .forUpdate()
                    .fetchOptional();

            // Now, send a transfer request from wallet_1 to wallet_2 (Connection B)
            // It should block on the lock and time out
            String idempotencyKey = "timeout-key-" + UUID.randomUUID();
            CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

            long startTime = System.currentTimeMillis();
            @SuppressWarnings("unchecked")
            ResponseEntity<Map<String, Object>> response =
                    (ResponseEntity) postTransfer(restTemplate, idempotencyKey, request, Map.class);
            long duration = System.currentTimeMillis() - startTime;

            // Verify it returned Gateway Timeout (504)
            assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
            assertThat(response.getBody()).isNotNull();
            assertThat(response.getBody().get("message").toString()).contains("timed out");

            // Verify B's transaction waited for roughly 1 second (our timeout
            // configuration) — both bounds are load-bearing.
            //
            // Floor (>= 1000ms): catches a regression that returns 504 too
            // eagerly without ever waiting for the lock; without it, a "fail
            // open" change could pass.
            //
            // Ceiling (< 5000ms): catches a regression that quietly raises
            // transfer.transaction.timeout-seconds (e.g. to the default 10s)
            // — the test would still pass on correctness but would burn the
            // extra time on every CI run. 5s is comfortably above the 1s
            // configured value plus normal scheduling jitter.
            assertThat(duration)
                    .isGreaterThanOrEqualTo(1000L)
                    .isLessThan(5000L);

            // Verify the idempotency key was rolled back (does not exist in DB)
            Optional<IdempotencyRecord> record = idempotencyRecordRepository.findById("test-client", idempotencyKey);
            assertThat(record).isEmpty();

        } finally {
            // Commit/rollback connection A to release the lock
            transactionManager.rollback(status);
        }
    }
}
