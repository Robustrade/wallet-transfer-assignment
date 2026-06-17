package com.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.wallet.api.dto.CreateTransferRequest;
import com.wallet.api.dto.TransferResponse;
import com.wallet.domain.TransferStatus;
import com.wallet.repository.WalletRepository;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

class ConcurrencyIntegrationTest extends AbstractIntegrationTest {

    @Autowired private TestRestTemplate restTemplate;

    @Autowired private WalletRepository walletRepository;

    @Test
    void preventsDoubleSpendUnderConcurrentDebits() throws Exception {
        walletRepository.insert("concurrent_wallet_a", 1_000L);
        walletRepository.insert("concurrent_wallet_b", 0L);

        int transferCount = 10;
        long amountPerTransfer = 100L;
        ExecutorService executor = Executors.newFixedThreadPool(transferCount);
        List<Callable<ResponseEntity<TransferResponse>>> tasks = new ArrayList<>();

        for (int i = 0; i < transferCount; i++) {
            String idempotencyKey = "concurrent-transfer-" + i;
            CreateTransferRequest request =
                    new CreateTransferRequest(
                            idempotencyKey, "concurrent_wallet_a", "concurrent_wallet_b", amountPerTransfer);
            tasks.add(() -> restTemplate.postForEntity("/transfers", request, TransferResponse.class));
        }

        List<Future<ResponseEntity<TransferResponse>>> futures = executor.invokeAll(tasks);
        executor.shutdown();

        long processedCount = 0;
        long failedCount = 0;
        for (Future<ResponseEntity<TransferResponse>> future : futures) {
            ResponseEntity<TransferResponse> response = future.get();
            if (response.getStatusCode() == HttpStatus.CREATED) {
                processedCount++;
                assertThat(response.getBody().status()).isEqualTo(TransferStatus.PROCESSED);
            } else {
                failedCount++;
                assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
                assertThat(response.getBody().status()).isEqualTo(TransferStatus.FAILED);
            }
        }

        assertThat(processedCount).isEqualTo(10);
        assertThat(failedCount).isZero();
        assertThat(walletRepository.findById("concurrent_wallet_a").orElseThrow().balance())
                .isZero();
        assertThat(walletRepository.findById("concurrent_wallet_b").orElseThrow().balance())
                .isEqualTo(1_000L);
    }
}
