package com.api.wallet.concurrency;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;

import com.api.wallet.TestcontainersConfiguration;
import com.api.wallet.domain.Wallet;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.repository.LedgerEntryRepository;
import com.api.wallet.repository.TransferRepository;
import com.api.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.webmvc.test.autoconfigure.AutoConfigureMockMvc;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import tools.jackson.databind.json.JsonMapper;

@SpringBootTest
@AutoConfigureMockMvc
@Import(TestcontainersConfiguration.class)
class ConcurrentTransferTest {

    private static final int CONCURRENT_REQUESTS = 20;

    @Autowired private MockMvc mockMvc;
    @Autowired private JsonMapper jsonMapper;
    @Autowired private WalletRepository walletRepository;
    @Autowired private TransferRepository transferRepository;
    @Autowired private LedgerEntryRepository ledgerEntryRepository;

    @Test
    void concurrentTransfersFromTheSameWalletNeverOverdrawOrLoseUpdates() throws Exception {
        String fromWalletId = "wallet-" + UUID.randomUUID();
        String toWalletId = "wallet-" + UUID.randomUUID();
        walletRepository.save(new Wallet(fromWalletId, new BigDecimal("1000.00")));
        walletRepository.save(new Wallet(toWalletId, new BigDecimal("0.00")));

        BigDecimal amountEach = new BigDecimal("10.00");
        List<Callable<Integer>> tasks = new ArrayList<>();
        for (int i = 0; i < CONCURRENT_REQUESTS; i++) {
            String idempotencyKey = "concurrent-distinct-" + i + "-" + UUID.randomUUID();
            tasks.add(
                    () -> {
                        String body =
                                jsonMapper.writeValueAsString(
                                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, amountEach));
                        return mockMvc
                                .perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(body))
                                .andReturn()
                                .getResponse()
                                .getStatus();
                    });
        }

        List<Integer> statusCodes = runConcurrently(tasks);

        assertThat(statusCodes).allMatch(status -> status == 201);

        BigDecimal expectedDebited = amountEach.multiply(BigDecimal.valueOf(CONCURRENT_REQUESTS));
        assertThat(walletRepository.findById(fromWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo(new BigDecimal("1000.00").subtract(expectedDebited));
        assertThat(walletRepository.findById(toWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo(expectedDebited);

        assertThat(ledgerEntryRepository.findByWalletIdOrderByCreatedAtDesc(fromWalletId))
                .hasSize(CONCURRENT_REQUESTS);
        assertThat(ledgerEntryRepository.findByWalletIdOrderByCreatedAtDesc(toWalletId))
                .hasSize(CONCURRENT_REQUESTS);
    }

    @Test
    void concurrentRequestsWithTheSameIdempotencyKeyProduceExactlyOneTransfer() throws Exception {
        String fromWalletId = "wallet-" + UUID.randomUUID();
        String toWalletId = "wallet-" + UUID.randomUUID();
        walletRepository.save(new Wallet(fromWalletId, new BigDecimal("1000.00")));
        walletRepository.save(new Wallet(toWalletId, new BigDecimal("0.00")));

        String idempotencyKey = UUID.randomUUID().toString();
        BigDecimal amount = new BigDecimal("50.00");

        List<Callable<String>> tasks = new ArrayList<>();
        for (int i = 0; i < CONCURRENT_REQUESTS; i++) {
            tasks.add(
                    () -> {
                        String body =
                                jsonMapper.writeValueAsString(
                                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, amount));
                        return mockMvc
                                .perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(body))
                                .andReturn()
                                .getResponse()
                                .getContentAsString();
                    });
        }

        List<String> responses = runConcurrently(tasks);

        assertThat(responses).allMatch(response -> response.equals(responses.get(0)));

        long transferCount =
                transferRepository.findAll().stream()
                        .filter(transfer -> transfer.getIdempotencyKey().equals(idempotencyKey))
                        .count();
        assertThat(transferCount).isEqualTo(1);

        assertThat(walletRepository.findById(fromWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo("950.00");
    }

    private static <T> List<T> runConcurrently(List<Callable<T>> tasks) throws Exception {
        ExecutorService executor = Executors.newFixedThreadPool(tasks.size());
        CountDownLatch ready = new CountDownLatch(tasks.size());
        CountDownLatch start = new CountDownLatch(1);

        List<Future<T>> futures = new ArrayList<>();
        for (Callable<T> task : tasks) {
            futures.add(
                    executor.submit(
                            () -> {
                                ready.countDown();
                                start.await();
                                return task.call();
                            }));
        }

        ready.await();
        start.countDown();

        List<T> results = new ArrayList<>();
        try {
            for (Future<T> future : futures) {
                results.add(future.get(30, TimeUnit.SECONDS));
            }
        } finally {
            executor.shutdown();
        }
        return results;
    }
}
