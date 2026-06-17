package com.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.wallet.api.dto.CreateTransferRequest;
import com.wallet.api.dto.TransferDetailResponse;
import com.wallet.api.dto.TransferResponse;
import com.wallet.api.dto.WalletResponse;
import com.wallet.domain.TransferStatus;
import com.wallet.repository.LedgerRepository;
import com.wallet.repository.WalletRepository;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpMethod;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

class TransferIntegrationTest extends AbstractIntegrationTest {

    @Autowired private TestRestTemplate restTemplate;

    @Autowired private WalletRepository walletRepository;

    @Autowired private LedgerRepository ledgerRepository;

    @Test
    void createsTransferWithDoubleEntryLedger() {
        long wallet1Before = walletRepository.findById("wallet_1").orElseThrow().balance();
        long wallet2Before = walletRepository.findById("wallet_2").orElseThrow().balance();

        CreateTransferRequest request =
                new CreateTransferRequest("integration-happy-1", "wallet_1", "wallet_2", 100L);

        ResponseEntity<TransferResponse> response =
                restTemplate.postForEntity("/transfers", request, TransferResponse.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(response.getBody().amount()).isEqualTo(100L);

        assertThat(walletRepository.findById("wallet_1").orElseThrow().balance())
                .isEqualTo(wallet1Before - 100);
        assertThat(walletRepository.findById("wallet_2").orElseThrow().balance())
                .isEqualTo(wallet2Before + 100);

        UUID transferId = UUID.fromString(response.getBody().transferId());
        assertThat(ledgerRepository.countByTransferId(transferId)).isEqualTo(2);

        ResponseEntity<TransferDetailResponse> detail =
                restTemplate.getForEntity(
                        "/transfers/" + transferId, TransferDetailResponse.class);
        assertThat(detail.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(detail.getBody()).isNotNull();
        assertThat(detail.getBody().ledgerEntries()).hasSize(2);
    }

    @Test
    void replaysIdempotentRequestWithoutDuplicateSideEffects() {
        CreateTransferRequest request =
                new CreateTransferRequest("integration-idempotent-1", "wallet_1", "wallet_2", 50L);

        ResponseEntity<TransferResponse> first =
                restTemplate.postForEntity("/transfers", request, TransferResponse.class);
        assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);

        long wallet1AfterFirst = walletRepository.findById("wallet_1").orElseThrow().balance();
        UUID transferId = UUID.fromString(first.getBody().transferId());

        ResponseEntity<TransferResponse> second =
                restTemplate.postForEntity("/transfers", request, TransferResponse.class);
        assertThat(second.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(second.getBody()).isEqualTo(first.getBody());
        assertThat(walletRepository.findById("wallet_1").orElseThrow().balance())
                .isEqualTo(wallet1AfterFirst);
        assertThat(ledgerRepository.countByTransferId(transferId)).isEqualTo(2);
    }

    @Test
    void rejectsIdempotencyKeyReuseWithDifferentPayload() {
        CreateTransferRequest first =
                new CreateTransferRequest("integration-conflict-1", "wallet_1", "wallet_2", 25L);
        CreateTransferRequest conflicting =
                new CreateTransferRequest("integration-conflict-1", "wallet_1", "wallet_2", 30L);

        restTemplate.postForEntity("/transfers", first, TransferResponse.class);

        ResponseEntity<String> response =
                restTemplate.postForEntity("/transfers", conflicting, String.class);
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    }

    @Test
    void marksTransferFailedWhenInsufficientFunds() {
        CreateTransferRequest request =
                new CreateTransferRequest(
                        "integration-failed-1", "wallet_2", "wallet_1", 999_999L);

        long wallet2Before = walletRepository.findById("wallet_2").orElseThrow().balance();

        ResponseEntity<TransferResponse> response =
                restTemplate.postForEntity("/transfers", request, TransferResponse.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().status()).isEqualTo(TransferStatus.FAILED);
        assertThat(walletRepository.findById("wallet_2").orElseThrow().balance())
                .isEqualTo(wallet2Before);

        ResponseEntity<TransferResponse> replay =
                restTemplate.postForEntity("/transfers", request, TransferResponse.class);
        assertThat(replay.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(replay.getBody()).isEqualTo(response.getBody());
    }

    @Test
    void returnsWalletBalance() {
        ResponseEntity<WalletResponse> response =
                restTemplate.getForEntity("/wallets/wallet_1", WalletResponse.class);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().walletId()).isEqualTo("wallet_1");
        assertThat(response.getBody().balance()).isPositive();
    }

    @Test
    void returnsNotFoundForMissingWallet() {
        ResponseEntity<String> response =
                restTemplate.getForEntity("/wallets/missing-wallet", String.class);
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }

    @Test
    void returnsNotFoundForMissingTransfer() {
        UUID missingId = UUID.randomUUID();
        ResponseEntity<String> response =
                restTemplate.exchange(
                        "/transfers/" + missingId,
                        HttpMethod.GET,
                        HttpEntity.EMPTY,
                        String.class);
        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }
}
