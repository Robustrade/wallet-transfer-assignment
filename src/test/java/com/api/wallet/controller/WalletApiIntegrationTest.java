package com.api.wallet.controller;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.api.wallet.TestcontainersConfiguration;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.domain.Wallet;
import com.api.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
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
class WalletApiIntegrationTest {

    @Autowired private MockMvc mockMvc;
    @Autowired private JsonMapper jsonMapper;
    @Autowired private WalletRepository walletRepository;

    private String walletAId;
    private String walletBId;

    @BeforeEach
    void setUp() {
        walletAId = "wallet-" + UUID.randomUUID();
        walletBId = "wallet-" + UUID.randomUUID();
        walletRepository.save(new Wallet(walletAId, new BigDecimal("1000.00")));
        walletRepository.save(new Wallet(walletBId, new BigDecimal("500.00")));
    }

    @Test
    void walletEndpointReturnsBalanceReflectsTransfersAndReturnsNotFoundForUnknownId() throws Exception {
        mockMvc.perform(get("/wallets/{id}", walletAId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.id").value(walletAId))
                .andExpect(jsonPath("$.balance").value(1000.00));

        postTransfer(UUID.randomUUID().toString(), walletAId, walletBId, "100");

        mockMvc.perform(get("/wallets/{id}", walletAId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.balance").value(900.00));
        mockMvc.perform(get("/wallets/{id}", walletBId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.balance").value(600.00));

        mockMvc.perform(get("/wallets/{id}", "no_such_wallet")).andExpect(status().isNotFound());
    }

    @Test
    void transferHistoryIncludesBothLegsNewestFirstExcludesUnrelatedWalletsAndReturnsNotFoundForUnknownId()
            throws Exception {
        String unrelatedWalletId = "wallet-" + UUID.randomUUID();
        walletRepository.save(new Wallet(unrelatedWalletId, new BigDecimal("100.00")));

        String firstKey = UUID.randomUUID().toString();
        String secondKey = UUID.randomUUID().toString();
        postTransfer(firstKey, walletAId, walletBId, "100");
        postTransfer(secondKey, walletBId, walletAId, "50");

        mockMvc.perform(get("/wallets/{id}/transfers", walletAId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.length()").value(2))
                .andExpect(jsonPath("$[0].idempotencyKey").value(secondKey))
                .andExpect(jsonPath("$[1].idempotencyKey").value(firstKey));

        mockMvc.perform(get("/wallets/{id}/transfers", unrelatedWalletId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.length()").value(0));

        mockMvc.perform(get("/wallets/{id}/transfers", "no_such_wallet")).andExpect(status().isNotFound());
    }

    private void postTransfer(String idempotencyKey, String fromWalletId, String toWalletId, String amount)
            throws Exception {
        String requestBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, new BigDecimal(amount)));
        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isCreated());
    }
}
