package com.robustrade.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.handler.dto.WalletResponse;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;

class StatementApiIntegrationTest extends AbstractIntegrationTest {

    @Test
    void getStatement_byWalletId_afterTransfer_showsRunningBalances() throws Exception {
        WalletResponse from = createWallet(3001L, "1000.00");
        WalletResponse to = createWallet(3002L, "0.00");

        createTransfer("stmt-xfer-1", from.getWalletId(), to.getWalletId(), "100.00");

        StatementResponse fromStatement = getStatementByWalletId(from.getWalletId());
        assertThat(fromStatement.getUserId()).isEqualTo(3001L);
        assertThat(fromStatement.getCurrentBalance()).isEqualByComparingTo("900.00");
        assertThat(fromStatement.getEntries()).hasSize(2);
        assertThat(fromStatement.getEntries().get(0).getTransferType().name()).isEqualTo("CREDIT");
        assertThat(fromStatement.getEntries().get(0).getTransferId()).isNull();
        assertThat(fromStatement.getEntries().get(0).getAmount()).isEqualByComparingTo("1000.00");
        assertThat(fromStatement.getEntries().get(0).getPreviousBalance()).isEqualByComparingTo("0.00");
        assertThat(fromStatement.getEntries().get(0).getBalanceAfterTransfer())
                .isEqualByComparingTo("1000.00");
        assertThat(fromStatement.getEntries().get(1).getTransferType().name()).isEqualTo("DEBIT");
        assertThat(fromStatement.getEntries().get(1).getAmount()).isEqualByComparingTo("100.00");
        assertThat(fromStatement.getEntries().get(1).getPreviousBalance()).isEqualByComparingTo("1000.00");
        assertThat(fromStatement.getEntries().get(1).getBalanceAfterTransfer())
                .isEqualByComparingTo("900.00");

        StatementResponse toStatement = getStatementByWalletId(to.getWalletId());
        assertThat(toStatement.getCurrentBalance()).isEqualByComparingTo("100.00");
        // Zero opening balance → no funding row; only transfer credit.
        assertThat(toStatement.getEntries()).hasSize(1);
        assertThat(toStatement.getEntries().get(0).getTransferType().name()).isEqualTo("CREDIT");
        assertThat(toStatement.getEntries().get(0).getPreviousBalance()).isEqualByComparingTo("0.00");
        assertThat(toStatement.getEntries().get(0).getBalanceAfterTransfer())
                .isEqualByComparingTo("100.00");
    }

    @Test
    void getStatement_byUserId_returnsWalletStatement() throws Exception {
        WalletResponse wallet = createWallet(3003L, "250.00");

        StatementResponse statement = getStatementByUserId(3003L);

        assertThat(statement.getWalletId()).isEqualTo(wallet.getWalletId());
        assertThat(statement.getCurrentBalance()).isEqualByComparingTo("250.00");
        assertThat(statement.getEntries()).hasSize(1);
        assertThat(statement.getEntries().get(0).getTransferId()).isNull();
        assertThat(statement.getEntries().get(0).getTransferType().name()).isEqualTo("CREDIT");
        assertThat(statement.getEntries().get(0).getAmount()).isEqualByComparingTo("250.00");
        assertThat(statement.getEntries().get(0).getPreviousBalance()).isEqualByComparingTo("0.00");
        assertThat(statement.getEntries().get(0).getBalanceAfterTransfer())
                .isEqualByComparingTo("250.00");
    }

    @Test
    void getStatement_whenNeitherParamProvided_returnsBadRequest() throws Exception {
        mockMvc.perform(get("/statements"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"));
    }

    @Test
    void getStatement_whenWalletMissing_returnsNotFound() throws Exception {
        mockMvc.perform(get("/statements").param("walletId", "missing-wallet"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.errorCode").value("WALLET_NOT_FOUND"));
    }

    @Test
    void databaseRejectsSecondWalletForSameUserId() throws Exception {
        createWallet(3004L, "10.00");
        // One wallet per user is enforced by wallets_user_id_uk (service + DB).
        org.assertj.core.api.Assertions.assertThatThrownBy(() -> jdbcTemplate.update(
                """
                INSERT INTO wallets (id, user_id, balance, created_at, updated_at)
                VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
                """,
                "extra-wallet-3004",
                3004L,
                new BigDecimal("20.00")
        )).isInstanceOf(org.springframework.dao.DuplicateKeyException.class);
    }

    @Test
    void getStatement_whenWalletDoesNotBelongToUser_returnsBadRequest() throws Exception {
        WalletResponse wallet = createWallet(3005L, "10.00");
        createWallet(3006L, "10.00");

        mockMvc.perform(get("/statements")
                        .param("userId", "3006")
                        .param("walletId", wallet.getWalletId()))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("WALLET_USER_MISMATCH"));
    }

    @Test
    void getStatement_whenBothParamsMatch_returnsOk() throws Exception {
        WalletResponse wallet = createWallet(3007L, "33.00");

        mockMvc.perform(get("/statements")
                        .param("userId", "3007")
                        .param("walletId", wallet.getWalletId()))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.walletId").value(wallet.getWalletId()))
                .andExpect(jsonPath("$.userId").value(3007))
                .andExpect(jsonPath("$.currentBalance").value(33.00));
    }

    @Test
    void getStatement_underConcurrentTransfers_currentBalanceMatchesLedgerRunningBalance()
            throws Exception {
        WalletResponse from = createWallet(3010L, "500.00");
        WalletResponse to = createWallet(3011L, "0.00");

        ExecutorService pool = Executors.newFixedThreadPool(8);
        try {
            List<Callable<Void>> tasks = new ArrayList<>();
            for (int i = 0; i < 10; i++) {
                final int index = i;
                tasks.add(() -> {
                    performTransfer(
                            "stmt-lock-xfer-" + index,
                            from.getWalletId(),
                            to.getWalletId(),
                            "10.00"
                    ).andReturn();
                    return null;
                });
                tasks.add(() -> {
                    StatementResponse statement = getStatementByWalletId(from.getWalletId());
                    assertCurrentBalanceMatchesLedger(statement);
                    StatementResponse toStatement = getStatementByWalletId(to.getWalletId());
                    assertCurrentBalanceMatchesLedger(toStatement);
                    return null;
                });
            }
            for (Future<Void> future : pool.invokeAll(tasks)) {
                future.get(30, TimeUnit.SECONDS);
            }
        } finally {
            pool.shutdownNow();
        }

        StatementResponse finalFrom = getStatementByWalletId(from.getWalletId());
        StatementResponse finalTo = getStatementByWalletId(to.getWalletId());
        assertCurrentBalanceMatchesLedger(finalFrom);
        assertCurrentBalanceMatchesLedger(finalTo);
        assertThat(finalFrom.getCurrentBalance().add(finalTo.getCurrentBalance()))
                .isEqualByComparingTo("500.00");
    }

    private static void assertCurrentBalanceMatchesLedger(StatementResponse statement) {
        if (statement.getEntries() == null || statement.getEntries().isEmpty()) {
            return;
        }
        BigDecimal lastRunning = statement.getEntries()
                .get(statement.getEntries().size() - 1)
                .getBalanceAfterTransfer();
        assertThat(statement.getCurrentBalance())
                .as("currentBalance must match last ledger running balance for wallet %s",
                        statement.getWalletId())
                .isEqualByComparingTo(lastRunning);
    }
}
