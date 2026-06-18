package com.wallet.handler;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.math.BigDecimal;
import java.util.Map;
import java.util.UUID;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.AbstractIntegrationTest;
import com.wallet.domain.Wallet;
import com.wallet.repository.WalletRepository;

@AutoConfigureMockMvc
class TransferControllerIntegrationTest extends AbstractIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private ObjectMapper objectMapper;

    @Test
    void postTransfer_happyPath_returnsCreatedWithProcessedStatus() throws Exception {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String requestBody = objectMapper.writeValueAsString(Map.of(
                "idempotencyKey", UUID.randomUUID().toString(),
                "fromWalletId", source.getId().toString(),
                "toWalletId", destination.getId().toString(),
                "amount", 40));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status").value("PROCESSED"));
    }

    @Test
    void postTransfer_duplicateIdempotencyKey_secondCallReplaysWithOkStatus() throws Exception {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String requestBody = objectMapper.writeValueAsString(Map.of(
                "idempotencyKey", UUID.randomUUID().toString(),
                "fromWalletId", source.getId().toString(),
                "toWalletId", destination.getId().toString(),
                "amount", 40));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isCreated());

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.status").value("PROCESSED"));
    }

    @Test
    void postTransfer_invalidPayload_returnsBadRequest() throws Exception {
        String requestBody = objectMapper.writeValueAsString(Map.of(
                "idempotencyKey", "",
                "fromWalletId", UUID.randomUUID().toString(),
                "toWalletId", UUID.randomUUID().toString(),
                "amount", -5));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isBadRequest());
    }

    @Test
    void postTransfer_amountWithTooManyDecimals_returnsBadRequestNotSilentlyRounded() throws Exception {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String requestBody = objectMapper.writeValueAsString(Map.of(
                "idempotencyKey", UUID.randomUUID().toString(),
                "fromWalletId", source.getId().toString(),
                "toWalletId", destination.getId().toString(),
                "amount", new BigDecimal("10.12345")));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isBadRequest());
    }

    @Test
    void postTransfer_malformedJsonBody_returnsBadRequestNotServerError() throws Exception {
        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content("{ not valid json"))
                .andExpect(status().isBadRequest());
    }

    @Test
    void transfersEndpoint_wrongHttpMethod_returnsMethodNotAllowedNotServerError() throws Exception {
        mockMvc.perform(get("/transfers"))
                .andExpect(status().isMethodNotAllowed());
    }
}
