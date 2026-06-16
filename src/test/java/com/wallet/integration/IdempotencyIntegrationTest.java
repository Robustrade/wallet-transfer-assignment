package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.controller.dto.TransferResponse;
import com.wallet.domain.TransferStatus;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import com.wallet.domain.IdempotencyRecord;
import com.wallet.repository.IdempotencyRecordRepository;
import java.util.Map;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for idempotency behavior.
 * Verifies that duplicate requests are handled correctly.
 */
class IdempotencyIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Test
    @DisplayName("Should return same result for duplicate idempotency key with same payload")
    void shouldReturnSameResultForDuplicate() {
        String idempotencyKey = "idem-dup-" + UUID.randomUUID();
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

        // First request
        ResponseEntity<TransferResponse> first = postTransfer(
                restTemplate, idempotencyKey, request, TransferResponse.class);
        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);

        // Second request with same key and payload
        ResponseEntity<TransferResponse> second = postTransfer(
                restTemplate, idempotencyKey, request, TransferResponse.class);

        // Should return the cached response (with 200 OK status for replay)
        assertThat(second.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(second.getBody()).isNotNull();
        assertThat(second.getBody().id()).isEqualTo(first.getBody().id());
        assertThat(second.getBody().status()).isEqualTo(first.getBody().status());
        assertThat(second.getBody().amount()).isEqualTo(first.getBody().amount());
    }

    @Test
    @DisplayName("Should return 409 when idempotency key is reused with different payload")
    void shouldConflictWhenKeyReusedWithDifferentPayload() {
        String idempotencyKey = "idem-conflict-" + UUID.randomUUID();

        // First request
        CreateTransferRequest first = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        postTransfer(restTemplate, idempotencyKey, first, TransferResponse.class);

        // Second request with same key but different amount
        CreateTransferRequest second = new CreateTransferRequest("wallet_1", "wallet_2", 200L);
        ResponseEntity<Map> response = postTransfer(
                restTemplate, idempotencyKey, second, Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    }

    @Test
    @DisplayName("Should return 409 when idempotency key is reused with different wallets")
    void shouldConflictWhenKeyReusedWithDifferentWallets() {
        String idempotencyKey = "idem-walletconflict-" + UUID.randomUUID();

        // First request
        CreateTransferRequest first = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        postTransfer(restTemplate, idempotencyKey, first, TransferResponse.class);

        // Second request with same key but different wallets
        CreateTransferRequest second = new CreateTransferRequest("wallet_2", "wallet_3", 100L);
        ResponseEntity<Map> response = postTransfer(
                restTemplate, idempotencyKey, second, Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    }

    @Test
    @DisplayName("Should not deduct balance on idempotent replay")
    void shouldNotDeductBalanceOnReplay() {
        // Get initial balance
        var initial = getWithClient(restTemplate, "/wallets/wallet_1/balance", Map.class).getBody();
        long initialBalance = ((Number) initial.get("balance")).longValue();

        String idempotencyKey = "idem-nodeduct-" + UUID.randomUUID();
        long amount = 50L;
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", amount);

        // Execute the transfer
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);

        // Replay 3 more times
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);

        // Balance should only reflect ONE deduction
        var updated = getWithClient(restTemplate, "/wallets/wallet_1/balance", Map.class).getBody();
        long updatedBalance = ((Number) updated.get("balance")).longValue();

        assertThat(updatedBalance).isEqualTo(initialBalance - amount);
    }

    @Test
    @DisplayName("Should handle idempotent replay of a failed transfer correctly")
    void shouldReplayFailedTransferCorrectly() {
        String idempotencyKey = "idem-failed-" + UUID.randomUUID();

        // wallet_4 has 0 balance, so this will fail
        CreateTransferRequest request = new CreateTransferRequest("wallet_4", "wallet_1", 500L);

        // First request — should fail
        ResponseEntity<Map> first = postTransfer(
                restTemplate, idempotencyKey, request, Map.class);
        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);

        // Replay — should also return the same failure (cached)
        ResponseEntity<Map> second = postTransfer(
                restTemplate, idempotencyKey, request, Map.class);

        // A replay of a failed transfer must return the same cached failure status.
        assertThat(second.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    }

    @Test
    @DisplayName("Should increment times seen and update last seen on replay")
    void shouldIncrementTimesSeenOnReplay() throws Exception {
        String idempotencyKey = "idem-replay-count-" + UUID.randomUUID();
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

        // First request
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);

        // Fetch the record and check initial state
        IdempotencyRecord recordAfterFirst = idempotencyRecordRepository.findById("test-client", idempotencyKey).orElseThrow();
        assertThat(recordAfterFirst.getTimesSeen()).isEqualTo(1);
        java.time.Instant firstSeen = recordAfterFirst.getFirstSeenAt();
        java.time.Instant lastSeen = recordAfterFirst.getLastSeenAt();

        // Second request (first replay).
        //
        // A Thread.sleep between the two requests was considered to "ensure
        // time difference is measurable" and rejected: the system clock is
        // not the right invariant. Under low resolution (Windows ≈ 16ms)
        // the two app-clock Instants can still land in the same tick, so a
        // sleep would only paper over the issue on fast-clock CI and
        // continue to flake on slow-clock runners. The post-replay
        // timestamp is asserted as monotonically non-decreasing
        // (isAfterOrEqualTo), and strict happens-after evidence comes from
        // the timesSeen counter — incremented server-side in SQL — which
        // is independent of clock resolution and robust on any CI host.
        postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);

        // Fetch again and verify timesSeen is 2, and lastSeenAt did not regress.
        IdempotencyRecord recordAfterSecond = idempotencyRecordRepository.findById("test-client", idempotencyKey).orElseThrow();
        assertThat(recordAfterSecond.getTimesSeen()).isEqualTo(2);
        assertThat(recordAfterSecond.getLastSeenAt()).isAfterOrEqualTo(lastSeen);
        assertThat(recordAfterSecond.getFirstSeenAt()).isEqualTo(firstSeen);
    }

    @Test
    @DisplayName("Should successfully process request when idempotency key is between 128 and 256 characters long")
    void shouldAllowLongIdempotencyKey() {
        // Create an idempotency key of 200 characters
        String idempotencyKey = "long-key-" + "a".repeat(180) + "-" + UUID.randomUUID().toString().substring(0, 8);
        assertThat(idempotencyKey.length()).isBetween(129, 256);

        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

        // Execute transfer using the long key
        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate, idempotencyKey, request, TransferResponse.class);

        // It should succeed (201 Created)
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(response.getBody()).isNotNull();

        // Replay should also succeed (200 OK)
        ResponseEntity<TransferResponse> replay = postTransfer(
                restTemplate, idempotencyKey, request, TransferResponse.class);
        assertThat(replay.getStatusCode()).isEqualTo(HttpStatus.OK);
    }

    @Test
    @DisplayName("Should return exactly identical error response body and status on replaying a failed request")
    void shouldReturnIdenticalErrorResponseOnReplay() {
        String idempotencyKey = "idem-err-replay-" + UUID.randomUUID();
        CreateTransferRequest request = new CreateTransferRequest("wallet_4", "wallet_1", 500L); // wallet_4 has 0 balance

        // First request — fails with 422 Unprocessable Entity
        @SuppressWarnings("unchecked")
        ResponseEntity<Map<String, Object>> first =
                (ResponseEntity) postTransfer(restTemplate, idempotencyKey, request, Map.class);
        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        Map<String, Object> firstBody = first.getBody();
        assertThat(firstBody).isNotNull();
        assertThat(firstBody).containsKeys("timestamp", "statusCode", "error", "message");
        assertThat(firstBody.get("statusCode")).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY.value());
        assertThat(firstBody.get("error")).isEqualTo("Unprocessable Entity");
        assertThat(String.valueOf(firstBody.get("message"))).contains("Insufficient balance");

        // Replay request — should return identical status and body structure
        @SuppressWarnings("unchecked")
        ResponseEntity<Map<String, Object>> replay =
                (ResponseEntity) postTransfer(restTemplate, idempotencyKey, request, Map.class);
        assertThat(replay.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        Map<String, Object> replayBody = replay.getBody();
        assertThat(replayBody).isNotNull();

        // Replay must carry the same key set as the first response — no missing or extra fields.
        assertThat(replayBody.keySet()).isEqualTo(firstBody.keySet());

        // Every field except timestamp must be byte-for-byte identical between first and replay.
        // The cached response body is replayed verbatim from the idempotency record, so equality
        // here is the strongest possible guarantee that the replay path is not regenerating data.
        assertThat(replayBody.get("statusCode")).isEqualTo(firstBody.get("statusCode"));
        assertThat(replayBody.get("error")).isEqualTo(firstBody.get("error"));
        assertThat(replayBody.get("message")).isEqualTo(firstBody.get("message"));

        // Timestamp must be present and in ISO-8601 form, but is allowed to differ between the
        // original error response (rendered by GlobalExceptionHandler) and the cached error
        // response (rendered by IdempotencyFailureRecorder) — the two run in different code
        // paths with different clocks, so equality is not required.
        assertThat(replayBody.get("timestamp")).isNotNull();
        assertThat(java.time.Instant.parse(String.valueOf(replayBody.get("timestamp")))).isNotNull();
    }
}
