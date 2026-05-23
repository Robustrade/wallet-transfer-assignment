package com.kulu.wallet;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.jdbc.core.JdbcTemplate;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

import static org.assertj.core.api.Assertions.assertThat;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class TransferIntegrationTest {

    // Fixed IDs reserved for tests — high enough to not clash with seed data
    private static final long W1 = 9001L;
    private static final long W2 = 9002L;
    private static final long W3 = 9003L;
    private static final long W4 = 9004L; // zero-balance wallet

    @LocalServerPort int port;
    @Autowired JdbcTemplate jdbc;

    private HttpClient httpClient;

    @BeforeEach
    void setUp() {
        httpClient = HttpClient.newHttpClient();

        // Remove ledger entries tied to any transfer involving test wallets
        jdbc.execute(
            "DELETE FROM ledger_entries WHERE transfer_id IN (" +
            "  SELECT id FROM transfers" +
            "  WHERE from_wallet_id IN (9001,9002,9003,9004)" +
            "     OR to_wallet_id   IN (9001,9002,9003,9004))");

        // Remove transfers involving test wallets
        jdbc.execute(
            "DELETE FROM transfers" +
            " WHERE from_wallet_id IN (9001,9002,9003,9004)" +
            "    OR to_wallet_id   IN (9001,9002,9003,9004)");

        // Remove idempotency records used by this test class
        jdbc.execute(
            "DELETE FROM idempotency_records" +
            " WHERE idempotency_key LIKE 'k%' OR idempotency_key LIKE 'conc-%'");

        // Reset test wallets to known starting balances
        jdbc.execute("DELETE FROM wallets WHERE id IN (9001,9002,9003,9004)");
        jdbc.execute(
            "INSERT INTO wallets (id, balance) VALUES" +
            " (9001,10000),(9002,10000),(9003,10000),(9004,0)");
    }

    @Test
    void happyPath_returns200_withCorrectBody() throws Exception {
        ApiResponse r = post("k1", W1, W2, 500);

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"status\":\"PROCESSED\"");
        assertThat(r.body).contains("\"fromWalletId\":" + W1);
        assertThat(r.body).contains("\"toWalletId\":"   + W2);
        assertThat(r.body).contains("\"amount\":500");
        assertThat(r.body).contains("\"transferId\"");

        assertThat(jdbc.queryForObject("SELECT balance FROM wallets WHERE id=9001", Long.class)).isEqualTo(9500L);
        assertThat(jdbc.queryForObject("SELECT balance FROM wallets WHERE id=9002", Long.class)).isEqualTo(10500L);
    }

    @Test
    void idempotency_sameKeyReturnsSameResponse_onlyOneTransferRow() throws Exception {
        ApiResponse first  = post("k2", W1, W2, 100);
        ApiResponse second = post("k2", W1, W2, 100);

        assertThat(first.status).isEqualTo(200);
        assertThat(second.status).isEqualTo(200);
        assertThat(first.body).isEqualTo(second.body);

        assertThat(jdbc.queryForObject(
            "SELECT COUNT(*) FROM transfers WHERE idempotency_key='k2'", Long.class))
            .isEqualTo(1L);
    }

    @Test
    void insufficientFunds_returns200_statusFailed_noLedgerEntries() throws Exception {
        ApiResponse r = post("k3", W4, W1, 1);

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"status\":\"FAILED\"");
        assertThat(r.body).contains("\"failureReason\":\"INSUFFICIENT_FUNDS\"");

        assertThat(jdbc.queryForObject(
            "SELECT status FROM transfers WHERE idempotency_key='k3'", String.class))
            .isEqualTo("FAILED");
        assertThat(jdbc.queryForObject(
            "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id IN" +
            " (SELECT id FROM transfers WHERE idempotency_key='k3')", Long.class))
            .isEqualTo(0L);
    }

    @Test
    void idempotency_duplicateKeyOnFailedTransfer_returns200_noNewTransferRow() throws Exception {
        assertThat(post("k4", W4, W1, 1).status).isEqualTo(200);
        assertThat(post("k4", W4, W1, 1).status).isEqualTo(200);

        assertThat(jdbc.queryForObject(
            "SELECT COUNT(*) FROM transfers WHERE idempotency_key='k4'", Long.class))
            .isEqualTo(1L);
    }

    @Test
    void walletNotFound_returns200_statusFailed_nullTransferId() throws Exception {
        ApiResponse r = post("k5", 99999L, W1, 100);

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"status\":\"FAILED\"");
        assertThat(r.body).contains("\"failureReason\":\"WALLET_NOT_FOUND\"");
        assertThat(r.body).contains("\"transferId\":null");
    }

    @Test
    void sameWallet_returns400() throws Exception {
        ApiResponse r = post("k6", W1, W1, 100);

        assertThat(r.status).isEqualTo(400);
        assertThat(r.body).contains("error");
    }

    @Test
    void missingFields_returns400() throws Exception {
        ApiResponse r = send("{\"idempotencyKey\":\"k7\",\"fromWalletId\":" + W1 + "}");

        assertThat(r.status).isEqualTo(400);
        assertThat(r.body).contains("error");
    }

    @Test
    void negativeAmount_returns400() throws Exception {
        ApiResponse r = send(String.format(
            "{\"idempotencyKey\":\"k8\",\"fromWalletId\":%d,\"toWalletId\":%d,\"amount\":-50}",
            W1, W2));

        assertThat(r.status).isEqualTo(400);
        assertThat(r.body).contains("error");
    }

    @Test
    void ledgerCorrectness_sumDebitEqualsSumCredit() throws Exception {
        assertThat(post("k9", W1, W2, 300).status).isEqualTo(200);

        String transferId = jdbc.queryForObject(
            "SELECT id FROM transfers WHERE idempotency_key='k9'", String.class);

        Long debitSum = jdbc.queryForObject(
            "SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE transfer_id=? AND type='DEBIT'",
            Long.class, transferId);
        Long creditSum = jdbc.queryForObject(
            "SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE transfer_id=? AND type='CREDIT'",
            Long.class, transferId);

        assertThat(debitSum).isEqualTo(creditSum).isEqualTo(300L);
    }

    @Test
    void concurrency_noDoubleSpend_tenSimultaneousTransfers() throws Exception {
        int threads = 10;
        int amount  = 1000;

        // Dedicated pool ensures all 10 tasks start immediately (common ForkJoinPool
        // has fewer threads on low-CPU machines, which would serialize some requests)
        ExecutorService executor = Executors.newFixedThreadPool(threads);
        List<CompletableFuture<Integer>> futures = new ArrayList<>();

        for (int i = 0; i < threads; i++) {
            String key = "conc-" + i;
            futures.add(CompletableFuture.supplyAsync(() -> {
                try {
                    return post(key, W1, W2, amount).status;
                } catch (Exception e) {
                    return 0;
                }
            }, executor));
        }

        // Wait for all to finish (or timeout after 30 s)
        CompletableFuture.allOf(futures.toArray(new CompletableFuture[0]))
            .get(30, TimeUnit.SECONDS);
        executor.shutdown();

        long successCount = futures.stream()
            .map(CompletableFuture::join)
            .filter(s -> s == 200)
            .count();

        long balance = jdbc.queryForObject("SELECT balance FROM wallets WHERE id=9001", Long.class);
        assertThat(balance).isEqualTo(10000L - successCount * amount);
        assertThat(balance).isGreaterThanOrEqualTo(0L);
        assertThat(jdbc.queryForObject(
            "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id IN" +
            " (SELECT id FROM transfers WHERE idempotency_key LIKE 'conc-%')", Long.class))
            .isEqualTo(successCount * 2);
    }

    // --- GET /wallets/{id}/balance ---

    @Test
    void getBalance_existingWallet_returns200_withBalance() throws Exception {
        ApiResponse r = get("/wallets/" + W1 + "/balance");

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"walletId\":" + W1);
        assertThat(r.body).contains("\"balance\":10000");
    }

    @Test
    void getBalance_afterTransfer_returnsUpdatedBalance() throws Exception {
        post("kb1", W1, W2, 1500);

        assertThat(get("/wallets/" + W1 + "/balance").body).contains("\"balance\":8500");
        assertThat(get("/wallets/" + W2 + "/balance").body).contains("\"balance\":11500");
    }

    @Test
    void getBalance_unknownWallet_returns404() throws Exception {
        ApiResponse r = get("/wallets/99999/balance");

        assertThat(r.status).isEqualTo(404);
        assertThat(r.body).contains("error");
    }

    // --- GET /transfers/{id} ---

    @Test
    void getTransfer_processedTransfer_returns200_withDetails() throws Exception {
        post("kt1", W1, W2, 200);
        String transferId = jdbc.queryForObject(
            "SELECT id FROM transfers WHERE idempotency_key='kt1'", String.class);

        ApiResponse r = get("/transfers/" + transferId);

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"id\":\"" + transferId + "\"");
        assertThat(r.body).contains("\"status\":\"PROCESSED\"");
        assertThat(r.body).contains("\"fromWalletId\":" + W1);
        assertThat(r.body).contains("\"toWalletId\":"   + W2);
        assertThat(r.body).contains("\"amount\":200");
    }

    @Test
    void getTransfer_failedTransfer_returns200_withFailureReason() throws Exception {
        post("kt2", W4, W1, 1);
        String transferId = jdbc.queryForObject(
            "SELECT id FROM transfers WHERE idempotency_key='kt2'", String.class);

        ApiResponse r = get("/transfers/" + transferId);

        assertThat(r.status).isEqualTo(200);
        assertThat(r.body).contains("\"status\":\"FAILED\"");
        assertThat(r.body).contains("\"failureReason\":\"INSUFFICIENT_FUNDS\"");
    }

    @Test
    void getTransfer_unknownId_returns404() throws Exception {
        ApiResponse r = get("/transfers/non-existent-id");

        assertThat(r.status).isEqualTo(404);
    }

    // --- helpers ---

    private ApiResponse post(String key, long from, long to, long amount) throws Exception {
        return send(String.format(
            "{\"idempotencyKey\":\"%s\",\"fromWalletId\":%d,\"toWalletId\":%d,\"amount\":%d}",
            key, from, to, amount));
    }

    private ApiResponse get(String path) throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create("http://localhost:" + port + path))
            .GET()
            .build();
        HttpResponse<String> response = httpClient.send(request, HttpResponse.BodyHandlers.ofString());
        return new ApiResponse(response.statusCode(), response.body());
    }

    private ApiResponse send(String json) throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create("http://localhost:" + port + "/transfers"))
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(json))
            .build();
        HttpResponse<String> response = httpClient.send(request, HttpResponse.BodyHandlers.ofString());
        return new ApiResponse(response.statusCode(), response.body());
    }

    private record ApiResponse(int status, String body) {}
}
