package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.controller.dto.TransferResponse;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.core.ParameterizedTypeReference;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import java.util.List;
import java.util.Map;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for read-only wallet endpoints — balance and transfer
 * history. Distinct from {@link TransferIntegrationTest}, which exercises the
 * write path on POST /transfers.
 *
 * <p>Every successful request carries {@code X-Client-Id: test-client}; the
 * controller now rejects unknown/missing clients to avoid leaking wallet
 * state to unauthenticated callers.
 *
 * <p>Class-level {@code @SuppressWarnings} mirrors {@link TransferIntegrationTest}:
 * error envelopes are accepted as raw {@code Map}, with assertions explicitly
 * coercing values via {@code String.valueOf} / {@code Number} casts.
 */
@SuppressWarnings({"unchecked", "rawtypes"})
class WalletQueryIntegrationTest extends BaseIntegrationTest {

    private static final String CLIENT_ID = "test-client";

    @Autowired
    private TestRestTemplate restTemplate;

    @Test
    @DisplayName("Should return wallet balance for an existing wallet")
    void shouldReturnBalanceForExistingWallet() {
        ResponseEntity<Map> response = restTemplate.exchange(
                "/wallets/wallet_1/balance",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("walletId")).isEqualTo("wallet_1");
        assertThat(((Number) response.getBody().get("balance")).longValue()).isEqualTo(1_000_000L);
    }

    @Test
    @DisplayName("Should return 404 when fetching balance for an unknown wallet")
    void shouldReturn404ForUnknownWalletBalance() {
        ResponseEntity<Map> response = restTemplate.exchange(
                "/wallets/wallet_does_not_exist/balance",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.NOT_FOUND.value());
    }

    @Test
    @DisplayName("Should return transfer history for a wallet that has transfers")
    void shouldReturnTransferHistoryForWalletWithTransfers() {
        // Arrange — drive two transfers through the API so the read path sees real rows.
        postTransfer(restTemplate, "history-1-" + UUID.randomUUID(),
                new CreateTransferRequest("wallet_1", "wallet_2", 100L), TransferResponse.class);
        postTransfer(restTemplate, "history-2-" + UUID.randomUUID(),
                new CreateTransferRequest("wallet_1", "wallet_3", 200L), TransferResponse.class);

        // Act
        ResponseEntity<List<TransferResponse>> response = restTemplate.exchange(
                "/wallets/wallet_1/transfers",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                new ParameterizedTypeReference<>() {});

        // Assert
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isNotNull().hasSize(2);
        assertThat(response.getBody()).allMatch(t ->
                "wallet_1".equals(t.fromWalletId()) && t.amount() != null && t.amount() > 0);
    }

    @Test
    @DisplayName("Should include incoming transfers in a wallet's history")
    void shouldIncludeIncomingTransfersInHistory() {
        // The repository selects FROM_WALLET_ID = ? OR TO_WALLET_ID = ?, so a
        // wallet's history must include transfers where it is the receiver, not
        // just the sender. The previous test only exercises the sender branch;
        // this one specifically targets the OR-receiver branch.
        postTransfer(restTemplate, "incoming-1-" + UUID.randomUUID(),
                new CreateTransferRequest("wallet_1", "wallet_2", 75L), TransferResponse.class);
        postTransfer(restTemplate, "incoming-2-" + UUID.randomUUID(),
                new CreateTransferRequest("wallet_3", "wallet_2", 25L), TransferResponse.class);

        ResponseEntity<List<TransferResponse>> response = restTemplate.exchange(
                "/wallets/wallet_2/transfers",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                new ParameterizedTypeReference<>() {});

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isNotNull().hasSize(2);
        // Every entry must touch wallet_2 — either as source or destination.
        assertThat(response.getBody()).allMatch(t ->
                "wallet_2".equals(t.fromWalletId()) || "wallet_2".equals(t.toWalletId()));
        // Both entries should be incoming for wallet_2 in this test.
        assertThat(response.getBody()).allMatch(t -> "wallet_2".equals(t.toWalletId()));
    }

    @Test
    @DisplayName("Should return empty list for an existing wallet with no transfers")
    void shouldReturnEmptyHistoryForWalletWithoutTransfers() {
        // wallet_4 starts at 0 balance and has no transfers in or out — exercises the
        // optimized path where transfers query returns empty but the wallet exists.
        ResponseEntity<List<TransferResponse>> response = restTemplate.exchange(
                "/wallets/wallet_4/transfers",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                new ParameterizedTypeReference<>() {});

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isNotNull().isEmpty();
    }

    @Test
    @DisplayName("Should return 404 when fetching transfer history for an unknown wallet")
    void shouldReturn404ForUnknownWalletHistory() {
        ResponseEntity<Map> response = restTemplate.exchange(
                "/wallets/wallet_does_not_exist/transfers",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders(CLIENT_ID)),
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.NOT_FOUND.value());
    }

    @Test
    @DisplayName("Should reject balance request without X-Client-Id (400)")
    void shouldReject400WhenClientIdHeaderMissingOnBalance() {
        // No auth headers at all — Spring's MissingRequestHeaderException must
        // surface as 400 via GlobalExceptionHandler, before any wallet lookup
        // can leak existence through the response code.
        ResponseEntity<Map> response = restTemplate.getForEntity(
                "/wallets/wallet_1/balance", Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(response.getBody()).isNotNull();
        assertThat((String) response.getBody().get("message")).contains("X-Client-Id");
    }

    @Test
    @DisplayName("Should reject transfer-history request without X-Client-Id (400)")
    void shouldReject400WhenClientIdHeaderMissingOnHistory() {
        ResponseEntity<Map> response = restTemplate.getForEntity(
                "/wallets/wallet_1/transfers", Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(response.getBody()).isNotNull();
        assertThat((String) response.getBody().get("message")).contains("X-Client-Id");
    }

    @Test
    @DisplayName("Should reject balance request from unknown X-Client-Id (403)")
    void shouldReject403WhenClientUnknownOnBalance() {
        // Existence of wallet_1 must NOT be revealed to a caller that has not
        // been registered — the response code must be the same whether the
        // wallet exists or not. Hence 403 here, not 404.
        ResponseEntity<Map> response = restTemplate.exchange(
                "/wallets/wallet_1/balance",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders("ghost-client")),
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.FORBIDDEN);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.FORBIDDEN.value());
    }

    @Test
    @DisplayName("Should reject transfer-history request from unknown X-Client-Id (403)")
    void shouldReject403WhenClientUnknownOnHistory() {
        ResponseEntity<Map> response = restTemplate.exchange(
                "/wallets/wallet_1/transfers",
                HttpMethod.GET,
                new HttpEntity<>(authHeaders("ghost-client")),
                Map.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.FORBIDDEN);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.FORBIDDEN.value());
    }

    private static HttpHeaders authHeaders(String clientId) {
        HttpHeaders headers = new HttpHeaders();
        headers.set("X-Client-Id", clientId);
        return headers;
    }
}
