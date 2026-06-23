package com.example.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;

import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.LedgerEntryRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.support.TestWalletFactory;
import com.example.wallet.web.dto.TransferRequest;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

@SpringBootTest
class TransferConcurrencyTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private TestWalletFactory testWalletFactory;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @BeforeEach
    void setUp() {
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();
        testWalletFactory.createWallet("wallet_a", new BigDecimal("1000.0000"));
        testWalletFactory.createWallet("wallet_b", new BigDecimal("0.0000"));
    }

    @Test
    void handlesConcurrentDebitsSafely() throws Exception {
        int transferCount = 20;
        BigDecimal transferAmount = new BigDecimal("10.0000");

        ExecutorService executorService = Executors.newFixedThreadPool(8);
        List<Callable<TransferExecutionResult>> tasks = new ArrayList<>();

        for (int index = 0; index < transferCount; index++) {
            int current = index;
            tasks.add(() -> transferService.createTransfer(new TransferRequest(
                    "concurrent-" + current, "wallet_a", "wallet_b", transferAmount)));
        }

        List<Future<TransferExecutionResult>> results = executorService.invokeAll(tasks);
        executorService.shutdown();

        long processedCount = results.stream()
                .map(future -> {
                    try {
                        return future.get();
                    } catch (Exception exception) {
                        throw new IllegalStateException(exception);
                    }
                })
                .filter(result -> result.body().status().name().equals("PROCESSED"))
                .count();

        Wallet sourceWallet = walletRepository.findById("wallet_a").orElseThrow();
        Wallet destinationWallet = walletRepository.findById("wallet_b").orElseThrow();

        assertThat(processedCount).isEqualTo(transferCount);
        assertThat(sourceWallet.getBalance()).isEqualByComparingTo(new BigDecimal("800.0000"));
        assertThat(destinationWallet.getBalance()).isEqualByComparingTo(new BigDecimal("200.0000"));
        assertThat(transferRepository.count()).isEqualTo(transferCount);
        assertThat(ledgerEntryRepository.count()).isEqualTo(transferCount * 2L);
    }
}
