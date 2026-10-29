package com.example.wallet.handler;

import static org.assertj.core.api.Assertions.assertThat;

import com.example.wallet.PostgresIntegrationTest;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/** Black-box HTTP tests: status codes, response bodies and replay behaviour. */
class TransferApiTest extends PostgresIntegrationTest {

    @Autowired TestRestTemplate http;

    private String wallet(long balance) {
        String id = uniqueId("api");
        ResponseEntity<WalletResponse> r =
                http.postForEntity("/wallets", Map.of("id", id, "openingBalance", balance), WalletResponse.class);
        assertThat(r.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        return id;
    }

    private ResponseEntity<TransferResponse> post(String key, String from, String to, Object amount) {
        return http.postForEntity(
                "/transfers",
                Map.of("idempotencyKey", key, "fromWalletId", from, "toWalletId", to, "amount", amount),
                TransferResponse.class);
    }

    private long balance(String id) {
        return http.getForObject("/wallets/" + id, WalletResponse.class).balance();
    }

    @Test
    void createTransferReturns201ThenReplayReturns200WithSameBody() {
        String a = wallet(300);
        String b = wallet(0);
        String key = uniqueId("k");

        ResponseEntity<TransferResponse> first = post(key, a, b, 100);
        ResponseEntity<TransferResponse> replay = post(key, a, b, 100);

        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(first.getBody().status()).isEqualTo("PROCESSED");
        assertThat(replay.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(replay.getBody().transferId()).isEqualTo(first.getBody().transferId());
        assertThat(balance(a)).isEqualTo(200);
        assertThat(balance(b)).isEqualTo(100);
    }

    @Test
    void ledgerEntriesEndpointShowsDebitAndCredit() {
        String a = wallet(300);
        String b = wallet(0);
        TransferResponse t = post(uniqueId("k"), a, b, 100).getBody();

        LedgerEntryResponse[] entries = http.getForObject(
                "/transfers/" + t.transferId() + "/ledger-entries", LedgerEntryResponse[].class);

        assertThat(entries).hasSize(2);
        assertThat(entries).extracting(LedgerEntryResponse::type).containsExactlyInAnyOrder("DEBIT", "CREDIT");
        assertThat(entries).extracting(LedgerEntryResponse::amount).containsOnly(100L);
    }

    @Test
    void insufficientFundsReturns422AndSameAnswerOnReplay() {
        String a = wallet(10);
        String b = wallet(0);
        String key = uniqueId("k");

        ResponseEntity<TransferResponse> first = post(key, a, b, 100);
        ResponseEntity<TransferResponse> replay = post(key, a, b, 100);

        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(first.getBody().status()).isEqualTo("FAILED");
        assertThat(first.getBody().failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
        assertThat(replay.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(replay.getBody().transferId()).isEqualTo(first.getBody().transferId());
        assertThat(balance(a)).isEqualTo(10);
    }

    @Test
    void sameKeyWithDifferentPayloadReturns422() {
        String a = wallet(300);
        String b = wallet(0);
        String key = uniqueId("k");
        post(key, a, b, 100);

        ResponseEntity<ErrorResponse> r = http.postForEntity(
                "/transfers",
                Map.of("idempotencyKey", key, "fromWalletId", a, "toWalletId", b, "amount", 150),
                ErrorResponse.class);

        assertThat(r.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(r.getBody().code()).isEqualTo("IDEMPOTENCY_KEY_REUSED");
    }

    @Test
    void invalidPayloadsReturn400() {
        String a = wallet(100);
        String b = wallet(0);

        assertThat(post(uniqueId("k"), a, b, 0).getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(post(uniqueId("k"), a, b, -5).getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(post(uniqueId("k"), a, a, 5).getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(http.postForEntity("/transfers", Map.of("amount", 5), ErrorResponse.class).getStatusCode())
                .isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    void unknownWalletReturns404() {
        String a = wallet(100);
        assertThat(post(uniqueId("k"), a, "nope-" + uniqueId("x"), 5).getStatusCode())
                .isEqualTo(HttpStatus.NOT_FOUND);
        assertThat(http.getForEntity("/wallets/nope-" + uniqueId("x"), ErrorResponse.class).getStatusCode())
                .isEqualTo(HttpStatus.NOT_FOUND);
    }

    @Test
    void duplicateWalletReturns409() {
        String a = wallet(1);
        ResponseEntity<ErrorResponse> r = http.postForEntity(
                "/wallets", Map.of("id", a, "openingBalance", 1), ErrorResponse.class);
        assertThat(r.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    }

    @Test
    void transferHistoryListsTransfersForEitherSide() {
        String a = wallet(300);
        String b = wallet(0);
        post(uniqueId("k"), a, b, 10);
        post(uniqueId("k"), a, b, 20);

        assertThat(http.getForObject("/wallets/" + a + "/transfers", TransferResponse[].class)).hasSize(2);
        assertThat(http.getForObject("/wallets/" + b + "/transfers", TransferResponse[].class)).hasSize(2);
    }
}
