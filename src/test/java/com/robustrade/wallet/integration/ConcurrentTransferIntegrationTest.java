package com.robustrade.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.handler.dto.WalletResponse;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.springframework.test.web.servlet.MvcResult;

class ConcurrentTransferIntegrationTest extends AbstractIntegrationTest {

    @Test
    void concurrentTransfers_doNotOverdrawSourceWallet() throws Exception {
        WalletResponse from = createWallet(4001L, "100.00");
        WalletResponse to = createWallet(4002L, "0.00");

        int attempts = 20;
        BigDecimal amount = new BigDecimal("10.00");
        ExecutorService pool = Executors.newFixedThreadPool(4);
        AtomicInteger successCount = new AtomicInteger();
        AtomicInteger insufficientCount = new AtomicInteger();

        try {
            List<Callable<Void>> tasks = new ArrayList<>();
            for (int i = 0; i < attempts; i++) {
                final int index = i;
                tasks.add(() -> {
                    MvcResult result = performTransfer(
                            "concurrent-" + index,
                            from.getWalletId(),
                            to.getWalletId(),
                            amount.toPlainString()
                    ).andReturn();

                    int status = result.getResponse().getStatus();
                    if (status == 201) {
                        successCount.incrementAndGet();
                    } else if (status == 400 && "INSUFFICIENT_BALANCE".equals(errorCode(result))) {
                        insufficientCount.incrementAndGet();
                    } else {
                        throw new AssertionError(
                                "Unexpected status=" + status + " body=" + result.getResponse().getContentAsString()
                        );
                    }
                    return null;
                });
            }

            List<Future<Void>> futures = pool.invokeAll(tasks);
            for (Future<Void> future : futures) {
                future.get(30, TimeUnit.SECONDS);
            }
        } finally {
            pool.shutdownNow();
        }

        assertThat(successCount.get()).isEqualTo(10);
        assertThat(insufficientCount.get()).isEqualTo(10);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("0.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("100.00");

        Integer ledgerRows = jdbcTemplate.queryForObject("SELECT COUNT(*) FROM ledger_entries", Integer.class);
        // 1 opening funding (from) + 10 successful transfers × 2 (debit+credit)
        assertThat(ledgerRows).isEqualTo(21);
    }

    @Test
    void endToEnd_createWalletsTransferAndReadStatements() throws Exception {
        WalletResponse alice = createWallet(4101L, "1000.00");
        WalletResponse bob = createWallet(4102L, "100.00");

        TransferResponse first = createTransfer("e2e-1", alice.getWalletId(), bob.getWalletId(), "250.00");
        TransferResponse second = createTransfer("e2e-2", bob.getWalletId(), alice.getWalletId(), "50.00");

        assertThat(first.getState().name()).isEqualTo("PROCESSED");
        assertThat(second.getState().name()).isEqualTo("PROCESSED");
        assertThat(walletBalance(alice.getWalletId())).isEqualByComparingTo("800.00");
        assertThat(walletBalance(bob.getWalletId())).isEqualByComparingTo("300.00");

        StatementResponse aliceStatement = getStatementByWalletId(alice.getWalletId());
        assertThat(aliceStatement.getEntries()).hasSize(3);
        assertThat(aliceStatement.getCurrentBalance()).isEqualByComparingTo("800.00");
        assertThat(aliceStatement.getEntries().get(0).getBalanceAfterTransfer())
                .isEqualByComparingTo("1000.00");
        assertThat(aliceStatement.getEntries().get(1).getBalanceAfterTransfer())
                .isEqualByComparingTo("750.00");
        assertThat(aliceStatement.getEntries().get(2).getBalanceAfterTransfer())
                .isEqualByComparingTo("800.00");

        StatementResponse bobStatement = getStatementByUserId(4102L);
        assertThat(bobStatement.getWalletId()).isEqualTo(bob.getWalletId());
        assertThat(bobStatement.getCurrentBalance()).isEqualByComparingTo("300.00");
        assertThat(bobStatement.getEntries()).hasSize(3);
    }
}
