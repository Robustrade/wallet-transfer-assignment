package com.robustrade.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.robustrade.wallet.handler.dto.CreateWalletRequest;
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
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MvcResult;

class WalletApiIntegrationTest extends AbstractIntegrationTest {

    @Test
    void createWallet_persistsAndReturnsCreatedWallet() throws Exception {
        WalletResponse response = createWallet(1001L, "1000.00");

        assertThat(response.getWalletId()).isNotBlank();
        assertThat(response.getUserId()).isEqualTo(1001L);
        assertThat(response.getBalance()).isEqualByComparingTo("1000.00");
        assertThat(response.getCreatedAt()).isNotNull();
        assertThat(walletBalance(response.getWalletId())).isEqualByComparingTo("1000.00");
    }

    @Test
    void createWallet_whenUserAlreadyHasWallet_returnsConflict() throws Exception {
        createWallet(1001L, "100.00");

        CreateWalletRequest duplicate = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("50.00"))
                .build();

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(duplicate)))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.errorCode").value("WALLET_ALREADY_EXISTS"));
    }

    @Test
    void createWallet_whenBalanceNegative_returnsBadRequest() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1002L)
                .balance(new BigDecimal("-1.00"))
                .build();

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"));
    }

    @Test
    void createWallet_whenBalanceHasTooManyFractionDigits_returnsValidationError() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1003L)
                .balance(new BigDecimal("100.001"))
                .build();

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("balance must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"));
    }

    @Test
    void createWallet_concurrentSameUserId_returnsCreatedOrConflictNeverServerError() throws Exception {
        long userId = 1099L;
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(userId)
                .balance(new BigDecimal("10.00"))
                .build();

        ExecutorService pool = Executors.newFixedThreadPool(8);
        AtomicInteger created = new AtomicInteger();
        AtomicInteger conflicts = new AtomicInteger();
        try {
            List<Callable<Void>> tasks = new ArrayList<>();
            for (int i = 0; i < 12; i++) {
                tasks.add(() -> {
                    MvcResult result = mockMvc.perform(post("/wallets")
                                    .contentType(MediaType.APPLICATION_JSON)
                                    .content(objectMapper.writeValueAsString(request)))
                            .andReturn();
                    int status = result.getResponse().getStatus();
                    if (status == 201) {
                        created.incrementAndGet();
                    } else if (status == 409) {
                        conflicts.incrementAndGet();
                    } else {
                        throw new AssertionError(
                                "Unexpected status=" + status + " body="
                                        + result.getResponse().getContentAsString()
                        );
                    }
                    return null;
                });
            }
            for (Future<Void> future : pool.invokeAll(tasks)) {
                future.get(30, TimeUnit.SECONDS);
            }
        } finally {
            pool.shutdownNow();
        }

        assertThat(created.get()).isEqualTo(1);
        assertThat(conflicts.get()).isEqualTo(11);
        Integer walletRows = jdbcTemplate.queryForObject(
                "SELECT COUNT(*) FROM wallets WHERE user_id = ?",
                Integer.class,
                userId
        );
        assertThat(walletRows).isEqualTo(1);
    }
}
