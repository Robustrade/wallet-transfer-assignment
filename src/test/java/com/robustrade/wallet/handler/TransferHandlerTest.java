package com.robustrade.wallet.handler;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.service.TransferService;
import com.robustrade.wallet.service.TransferServiceException;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

@WebMvcTest(controllers = TransferHandler.class)
@Import(GlobalExceptionHandler.class)
class TransferHandlerTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @MockBean
    private TransferService transferService;

    @Test
    void createTransfer_validRequest_returnsCreated() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .build();

        TransferResponse response = TransferResponse.builder()
                .transferId(UUID.randomUUID())
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .state(TransferState.PROCESSED)
                .build();

        when(transferService.createTransfer(any(CreateTransferRequest.class))).thenReturn(response);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.idempotencyKey").value("key-1"))
                .andExpect(jsonPath("$.fromWalletId").value("wallet-a"))
                .andExpect(jsonPath("$.toWalletId").value("wallet-b"))
                .andExpect(jsonPath("$.state").value("PROCESSED"));

        verify(transferService).createTransfer(any(CreateTransferRequest.class));
    }

    @Test
    void createTransfer_whenWalletsNotFound_returnsNotFound() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .build();

        when(transferService.createTransfer(any(CreateTransferRequest.class)))
                .thenThrow(TransferServiceException.walletsNotFound());

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.errorCode").value("WALLETS_NOT_FOUND"));
    }

    @Test
    void createTransfer_whenIdempotencyConflict_returnsConflict() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .build();

        when(transferService.createTransfer(any(CreateTransferRequest.class)))
                .thenThrow(TransferServiceException.idempotencyConflict());

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.errorCode").value("IDEMPOTENCY_CONFLICT"));
    }

    @Test
    void createTransfer_whenInsufficientBalance_returnsBadRequest() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .build();

        when(transferService.createTransfer(any(CreateTransferRequest.class)))
                .thenThrow(TransferServiceException.insufficientBalance("Insufficient balance"));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("INSUFFICIENT_BALANCE"));
    }

    @Test
    void createTransfer_whenAmountInvalid_returnsBadRequest() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("0.00"))
                .build();

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("Invalid amount. Amount must be at least 0.01"));

        verify(transferService, never()).createTransfer(any());
    }

    @Test
    void createTransfer_whenAmountHasTooManyFractionDigits_returnsBadRequest() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("1.001"))
                .build();

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("amount must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"));

        verify(transferService, never()).createTransfer(any());
    }

    @Test
    void createTransfer_whenIdempotencyKeyTooLong_returnsBadRequest() throws Exception {
        String tooLongKey = "k".repeat(129);
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey(tooLongKey)
                .fromWalletId("wallet-a")
                .toWalletId("wallet-b")
                .amount(new BigDecimal("100.00"))
                .build();

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("idempotencyKey must be at most 128 characters"));

        verify(transferService, never()).createTransfer(any());
    }

    @Test
    void createTransfer_whenWalletsSame_returnsBadRequest() throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey("key-1")
                .fromWalletId("wallet-a")
                .toWalletId("wallet-a")
                .amount(new BigDecimal("100.00"))
                .build();

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("fromWalletId and toWalletId must be different"));

        verify(transferService, never()).createTransfer(any());
    }
}
