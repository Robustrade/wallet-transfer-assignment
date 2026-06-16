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

import java.util.Map;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for the core transfer flow.
 * Uses a real PostgreSQL instance via Testcontainers.
 *
 * <p>Class-level {@code @SuppressWarnings("unchecked")}: the error-path tests
 * accept the JSON error envelope as a raw {@code Map} and assert on a few
 * fields. Parameterising every {@code ResponseEntity<Map>} declaration as
 * {@code Map<String, Object>} adds noise without test value — the assertions
 * already coerce values explicitly via {@code String.valueOf} / {@code Number}
 * casts. The matching pattern in {@link IdempotencyIntegrationTest} suppresses
 * at the local-variable level; this file has many more sites, so the
 * class-level form keeps the diff small.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
class TransferIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Test
    @DisplayName("Should successfully transfer between two wallets")
    void shouldTransferSuccessfully() {
        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate,
                "transfer-success-" + UUID.randomUUID(),
                "wallet_1", "wallet_2", 500L,
                TransferResponse.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(response.getBody().fromWalletId()).isEqualTo("wallet_1");
        assertThat(response.getBody().toWalletId()).isEqualTo("wallet_2");
        assertThat(response.getBody().amount()).isEqualTo(500L);
        assertThat(response.getBody().id()).isNotNull();
    }

    @Test
    @DisplayName("Should return 422 when sender has insufficient balance")
    void shouldFailWithInsufficientBalance() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-insuff-" + UUID.randomUUID(),
                "wallet_4", "wallet_1", 100L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    }

    @Test
    @DisplayName("Should return 404 when source wallet does not exist")
    void shouldFailWhenSourceWalletNotFound() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-notfound-" + UUID.randomUUID(),
                "nonexistent_wallet", "wallet_1", 100L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }

    @Test
    @DisplayName("Should return 404 when destination wallet does not exist")
    void shouldFailWhenDestWalletNotFound() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-destnotfound-" + UUID.randomUUID(),
                "wallet_1", "nonexistent_wallet", 100L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }

    @Test
    @DisplayName("Should return 400 when source and destination wallets are the same")
    void shouldFailWhenSameWallet() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-same-" + UUID.randomUUID(),
                "wallet_1", "wallet_1", 100L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("Should return 400 when amount is zero")
    void shouldFailWithZeroAmount() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-zero-" + UUID.randomUUID(),
                "wallet_1", "wallet_2", 0L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("Should return 400 when amount is negative")
    void shouldFailWithNegativeAmount() {
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "transfer-neg-" + UUID.randomUUID(),
                "wallet_1", "wallet_2", -100L,
                Map.class
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("Should return 400 when idempotencyKey is blank")
    void shouldFailWithBlankIdempotencyKey() {
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        ResponseEntity<Map> response = postTransfer(restTemplate, "", request, Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("Should return 400 when idempotencyKey is whitespace-only")
    void shouldFailWithWhitespaceOnlyIdempotencyKey() {
        // The IdempotencyHeaderFilter rejects keys that are blank after trim. This is
        // distinct from the empty-string case above: a whitespace-only header is
        // syntactically present but semantically empty, and we must reject it before
        // the request reaches the database (where it would otherwise be accepted as a
        // valid distinct key).
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        ResponseEntity<Map> response = postTransfer(restTemplate, "   ", request, Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(response.getBody()).isNotNull();
        assertThat(String.valueOf(response.getBody().get("message")))
                .contains("Idempotency-Key header is required");
    }

    @Test
    @DisplayName("Should return 400 when idempotencyKey header is missing")
    void shouldFailWithMissingIdempotencyKeyHeader() {
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        ResponseEntity<Map> response = postTransfer(restTemplate, null, request, Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("Should update wallet balances correctly after transfer")
    void shouldUpdateBalancesCorrectly() {
        // Get initial balances
        var initialFrom = getWithClient(restTemplate, "/wallets/wallet_3/balance", Map.class).getBody();
        long initialFromBalance = ((Number) initialFrom.get("balance")).longValue();

        var initialTo = getWithClient(restTemplate, "/wallets/wallet_2/balance", Map.class).getBody();
        long initialToBalance = ((Number) initialTo.get("balance")).longValue();

        long transferAmount = 200L;

        // Execute transfer
        postTransfer(
                restTemplate,
                "transfer-balance-" + UUID.randomUUID(),
                "wallet_3", "wallet_2", transferAmount,
                TransferResponse.class
        );

        // Check updated balances
        var updatedFrom = getWithClient(restTemplate, "/wallets/wallet_3/balance", Map.class).getBody();
        long updatedFromBalance = ((Number) updatedFrom.get("balance")).longValue();

        var updatedTo = getWithClient(restTemplate, "/wallets/wallet_2/balance", Map.class).getBody();
        long updatedToBalance = ((Number) updatedTo.get("balance")).longValue();

        assertThat(updatedFromBalance).isEqualTo(initialFromBalance - transferAmount);
        assertThat(updatedToBalance).isEqualTo(initialToBalance + transferAmount);
    }

    @Test
    @DisplayName("Should allow same idempotency key for different clients")
    void shouldAllowSameIdempotencyKeyForDifferentClients() {
        String idempotencyKey = "client-scope-key-" + UUID.randomUUID();
        CreateTransferRequest request1 = new CreateTransferRequest("wallet_1", "wallet_2", 10L);
        CreateTransferRequest request2 = new CreateTransferRequest("wallet_1", "wallet_2", 20L);

        // Execute transfer for client1
        ResponseEntity<TransferResponse> res1 = postTransfer(
                restTemplate, "client-1", idempotencyKey, request1, TransferResponse.class);
        assertThat(res1.getStatusCode()).isEqualTo(HttpStatus.CREATED);

        // Execute transfer for client2 using the exact same idempotency key but different payload/amount
        ResponseEntity<TransferResponse> res2 = postTransfer(
                restTemplate, "client-2", idempotencyKey, request2, TransferResponse.class);
        assertThat(res2.getStatusCode()).isEqualTo(HttpStatus.CREATED);

        // Verify they are two different transfers
        assertThat(res1.getBody().id()).isNotEqualTo(res2.getBody().id());
        assertThat(res1.getBody().amount()).isEqualTo(10L);
        assertThat(res2.getBody().amount()).isEqualTo(20L);
    }

    @Test
    @DisplayName("Should return 403 when X-Client-Id refers to an unregistered client")
    void shouldReject403ForUnknownClient() {
        // The signature filter accepts the request because the HMAC is correct for
        // the supplied (unknown) client id. The TransferService then looks the
        // client up in the registry and rejects with 403 — a clean authorization
        // failure, not a downstream FK violation.
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);
        ResponseEntity<Map> response = postTransfer(
                restTemplate,
                "ghost-client-" + UUID.randomUUID(),
                "unknown-client-" + UUID.randomUUID(),
                request,
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.FORBIDDEN);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.FORBIDDEN.value());
        assertThat(response.getBody().get("error")).isEqualTo("Forbidden");
        assertThat(String.valueOf(response.getBody().get("message")))
                .contains("Unknown client");
    }

    @Test
    @DisplayName("Should return X-Request-Id and X-Correlation-Id headers in response")
    void shouldReturnTracingHeaders() {
        String correlationId = "test-correlation-id-" + UUID.randomUUID();
        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

        // Execute post request with correlationId header
        ResponseEntity<TransferResponse> response = postTransfer(
                restTemplate, "test-client", correlationId, "idem-tracing-" + UUID.randomUUID(), request, TransferResponse.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);

        // Assert tracing headers are present and correct
        String resCorrelationId = response.getHeaders().getFirst("X-Correlation-Id");
        String resRequestId = response.getHeaders().getFirst("X-Request-Id");

        assertThat(resCorrelationId).isEqualTo(correlationId);
        assertThat(resRequestId).isNotNull().isNotBlank();

        // Check UUID format of requestId
        assertThat(UUID.fromString(resRequestId)).isNotNull();
    }
}
