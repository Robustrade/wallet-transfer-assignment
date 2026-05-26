package com.wallet.controller;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.domain.TransferStatus;
import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.GlobalExceptionHandler;
import com.wallet.exception.InsufficientFundsException;
import com.wallet.exception.SelfTransferException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.service.TransferService;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

@WebMvcTest(TransferController.class)
@Import(GlobalExceptionHandler.class)
class TransferControllerTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @MockBean
    private TransferService transferService;

    @Test
    void createTransfer_validRequest_returns201() throws Exception {
        TransferResponse response = new TransferResponse(
            UUID.randomUUID(), "key-1", "w1", "w2",
            new BigDecimal("100.00"), TransferStatus.PROCESSED, null, Instant.now()
        );
        when(transferService.executeTransfer(any()))
            .thenReturn(TransferExecutionResult.newTransfer(response));

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", new BigDecimal("100.00")))))
            .andExpect(status().isCreated())
            .andExpect(jsonPath("$.status").value("PROCESSED"))
            .andExpect(jsonPath("$.idempotencyKey").value("key-1"));
    }

    @Test
    void createTransfer_duplicateKey_returns200() throws Exception {
        TransferResponse response = new TransferResponse(
            UUID.randomUUID(), "key-1", "w1", "w2",
            new BigDecimal("100.00"), TransferStatus.PROCESSED, null, Instant.now()
        );
        when(transferService.executeTransfer(any()))
            .thenReturn(TransferExecutionResult.duplicate(response));

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", new BigDecimal("100.00")))))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("PROCESSED"));
    }

    @Test
    void createTransfer_missingIdempotencyKey_returns400() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":100}"))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.code").value("VALIDATION_ERROR"));
    }

    @Test
    void createTransfer_negativeAmount_returns400() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", new BigDecimal("-10.00")))))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.code").value("VALIDATION_ERROR"));
    }

    @Test
    void createTransfer_zeroAmount_returns400() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", BigDecimal.ZERO))))
            .andExpect(status().isBadRequest());
    }

    @Test
    void createTransfer_selfTransfer_returns400() throws Exception {
        when(transferService.executeTransfer(any())).thenThrow(new SelfTransferException("w1"));

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w1", new BigDecimal("100.00")))))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.code").value("SELF_TRANSFER"));
    }

    @Test
    void createTransfer_walletNotFound_returns404() throws Exception {
        when(transferService.executeTransfer(any())).thenThrow(new WalletNotFoundException("w1"));

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", new BigDecimal("100.00")))))
            .andExpect(status().isNotFound())
            .andExpect(jsonPath("$.code").value("WALLET_NOT_FOUND"));
    }

    @Test
    void createTransfer_insufficientFunds_returns422() throws Exception {
        when(transferService.executeTransfer(any()))
            .thenThrow(new InsufficientFundsException("Wallet w1 has insufficient balance"));

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(
                    new TransferRequest("key-1", "w1", "w2", new BigDecimal("9999.00")))))
            .andExpect(status().isUnprocessableEntity())
            .andExpect(jsonPath("$.code").value("INSUFFICIENT_FUNDS"));
    }

    @Test
    void createTransfer_missingBody_returns400() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON))
            .andExpect(status().isBadRequest());
    }

    @Test
    void getTransfer_validId_returns200() throws Exception {
        UUID id = UUID.randomUUID();
        TransferResponse response = new TransferResponse(
            id, "key-1", "w1", "w2",
            new BigDecimal("100.00"), TransferStatus.PROCESSED, null, Instant.now()
        );
        when(transferService.getTransfer(id)).thenReturn(response);

        mockMvc.perform(get("/transfers/" + id))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.transferId").value(id.toString()));
    }
}
