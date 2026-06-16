package com.wallet.integration;

import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;

import org.springframework.beans.factory.annotation.Autowired;
import org.junit.jupiter.api.BeforeEach;

/**
 * Base class for integration tests using Testcontainers with a real PostgreSQL instance.
 *
 * <p>All integration tests extend this class to share a single Postgres container,
 * which is faster than starting a new container per test class. Per-test cleanup
 * uses {@code TRUNCATE ... CASCADE} rather than {@code flyway.clean()} +
 * {@code migrate()} — the schema is created once at container start, and every
 * subsequent test simply empties the data tables. The cost difference is
 * roughly an order of magnitude per test, which adds up across the suite.
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles("test")
public abstract class BaseIntegrationTest {

    @Autowired
    private JdbcTemplate jdbcTemplate;

    @Autowired
    protected com.fasterxml.jackson.databind.ObjectMapper objectMapper;

    protected static final String TEST_SECRET = "my-super-secret-key-that-is-at-least-256-bits-long-for-hmac-sha256";

    @BeforeEach
    void resetDatabase() {
        // Truncate every table in the application schema in a single statement so
        // FK ordering is irrelevant. RESTART IDENTITY zeroes out any sequence-
        // backed identity columns (none today, but defensive against future
        // additions).
        jdbcTemplate.execute(
                "TRUNCATE TABLE ledger_entries, transfers, idempotency_records, wallets, clients " +
                        "RESTART IDENTITY CASCADE");

        // Re-apply the test client seed that the repeatable migration installed
        // initially. Clients are referenced by FK from idempotency_records, so
        // every test needs them present.
        jdbcTemplate.update("INSERT INTO clients (id, name) VALUES (?, ?)", "test-client", "Test Client");
        jdbcTemplate.update("INSERT INTO clients (id, name) VALUES (?, ?)", "client-1", "Client 1");
        jdbcTemplate.update("INSERT INTO clients (id, name) VALUES (?, ?)", "client-2", "Client 2");

        // Re-seed test wallets. wallet_4 is intentionally left at 0 so tests can
        // exercise the insufficient-balance failure path.
        jdbcTemplate.update("INSERT INTO wallets (id, balance) VALUES (?, ?)", "wallet_1", 1_000_000L);
        jdbcTemplate.update("INSERT INTO wallets (id, balance) VALUES (?, ?)", "wallet_2", 1_000_000L);
        jdbcTemplate.update("INSERT INTO wallets (id, balance) VALUES (?, ?)", "wallet_3",   500_000L);
        jdbcTemplate.update("INSERT INTO wallets (id, balance) VALUES (?, ?)", "wallet_4",         0L);
    }

    static final PostgreSQLContainer<?> postgres;

    static {
        postgres = new PostgreSQLContainer<>("postgres:16-alpine")
                .withDatabaseName("wallet_db_test")
                .withUsername("test_user")
                .withPassword("test_pass");
        postgres.start();
    }

    @DynamicPropertySource
    static void configureProperties(DynamicPropertyRegistry registry) {
        registry.add("spring.datasource.url", postgres::getJdbcUrl);
        registry.add("spring.datasource.username", postgres::getUsername);
        registry.add("spring.datasource.password", postgres::getPassword);
    }

    private String computeSignature(String method, String path, String clientId, String key, String timestamp, String body) {
        try {
            String bodyHash = sha256Hex(body);
            javax.crypto.spec.SecretKeySpec secretKeySpec = new javax.crypto.spec.SecretKeySpec(
                    TEST_SECRET.getBytes(java.nio.charset.StandardCharsets.UTF_8), "HmacSHA256");
            javax.crypto.Mac mac = javax.crypto.Mac.getInstance("HmacSHA256");
            mac.init(secretKeySpec);
            String data = method + ":" + path + ":" + clientId + ":" + key + ":" + timestamp + ":" + bodyHash;
            byte[] hmacBytes = mac.doFinal(data.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            return java.util.HexFormat.of().formatHex(hmacBytes);
        } catch (Exception e) {
            throw new RuntimeException("Failed to compute HMAC signature for testing", e);
        }
    }

    private static String sha256Hex(String input) {
        try {
            java.security.MessageDigest digest = java.security.MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(input.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            return java.util.HexFormat.of().formatHex(hash);
        } catch (Exception e) {
            throw new RuntimeException("Failed to compute SHA-256", e);
        }
    }

    /**
     * Issues an authenticated GET against a read-only wallet endpoint. The
     * controller now requires {@code X-Client-Id} on every request to avoid
     * leaking wallet existence to unauthenticated callers, so every test that
     * reads balance/history must use this helper (or set the header manually).
     */
    protected <T> org.springframework.http.ResponseEntity<T> getWithClient(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String clientId,
            String path,
            Class<T> responseType) {

        org.springframework.http.HttpHeaders headers = new org.springframework.http.HttpHeaders();
        headers.set("X-Client-Id", clientId);
        return restTemplate.exchange(
                path,
                org.springframework.http.HttpMethod.GET,
                new org.springframework.http.HttpEntity<>(headers),
                responseType);
    }

    /** Convenience overload defaulting to the seeded {@code test-client}. */
    protected <T> org.springframework.http.ResponseEntity<T> getWithClient(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String path,
            Class<T> responseType) {
        return getWithClient(restTemplate, "test-client", path, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransferWithHeaders(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            org.springframework.http.HttpHeaders headers,
            com.wallet.controller.dto.CreateTransferRequest request,
            Class<T> responseType) {
        
        org.springframework.http.HttpEntity<com.wallet.controller.dto.CreateTransferRequest> entity = 
                new org.springframework.http.HttpEntity<>(request, headers);
        
        return restTemplate.postForEntity("/transfers", entity, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransfer(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String clientId,
            String correlationId,
            String idempotencyKey,
            com.wallet.controller.dto.CreateTransferRequest request,
            Class<T> responseType) {
        
        org.springframework.http.HttpHeaders headers = new org.springframework.http.HttpHeaders();
        if (clientId != null) {
            headers.set("X-Client-Id", clientId);
        }
        if (correlationId != null) {
            headers.set("X-Correlation-Id", correlationId);
        }
        if (idempotencyKey != null) {
            headers.set("Idempotency-Key", idempotencyKey);
            String timestamp = String.valueOf(System.currentTimeMillis());
            headers.set("X-Timestamp", timestamp);
            
            String bodyJson;
            try {
                bodyJson = objectMapper.writeValueAsString(request);
            } catch (Exception e) {
                throw new RuntimeException(e);
            }
            
            String signature = computeSignature("POST", "/transfers", clientId != null ? clientId : "", idempotencyKey, timestamp, bodyJson);
            headers.set("X-Signature", signature);
        }
        
        return postTransferWithHeaders(restTemplate, headers, request, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransfer(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String clientId,
            String idempotencyKey,
            com.wallet.controller.dto.CreateTransferRequest request,
            Class<T> responseType) {
        return postTransfer(restTemplate, clientId, null, idempotencyKey, request, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransfer(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String idempotencyKey,
            com.wallet.controller.dto.CreateTransferRequest request,
            Class<T> responseType) {
        return postTransfer(restTemplate, "test-client", null, idempotencyKey, request, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransfer(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String idempotencyKey,
            String fromWalletId,
            String toWalletId,
            Long amount,
            Class<T> responseType) {
        
        com.wallet.controller.dto.CreateTransferRequest request = 
                new com.wallet.controller.dto.CreateTransferRequest(fromWalletId, toWalletId, amount);
        return postTransfer(restTemplate, "test-client", null, idempotencyKey, request, responseType);
    }

    protected <T> org.springframework.http.ResponseEntity<T> postTransfer(
            org.springframework.boot.test.web.client.TestRestTemplate restTemplate,
            String clientId,
            String idempotencyKey,
            String fromWalletId,
            String toWalletId,
            Long amount,
            Class<T> responseType) {
        
        com.wallet.controller.dto.CreateTransferRequest request = 
                new com.wallet.controller.dto.CreateTransferRequest(fromWalletId, toWalletId, amount);
        return postTransfer(restTemplate, clientId, null, idempotencyKey, request, responseType);
    }
}
