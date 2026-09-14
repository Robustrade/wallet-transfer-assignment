package com.robustrade.wallet.handler;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.WalletResponse;
import com.robustrade.wallet.service.WalletService;
import com.robustrade.wallet.service.WalletServiceException;
import java.math.BigDecimal;
import java.time.Instant;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

@WebMvcTest(controllers = WalletHandler.class)
@Import(GlobalExceptionHandler.class)
class WalletHandlerTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @MockBean
    private WalletService walletService;

    @Test
    void createWallet_validRequest_returnsCreated() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("1000.00"))
                .build();

        WalletResponse response = WalletResponse.builder()
                .walletId("wallet-123")
                .userId(1001L)
                .balance(new BigDecimal("1000.00"))
                .createdAt(Instant.parse("2026-09-13T00:00:00Z"))
                .build();

        when(walletService.createWallet(any(CreateWalletRequest.class))).thenReturn(response);

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.walletId").value("wallet-123"))
                .andExpect(jsonPath("$.userId").value(1001))
                .andExpect(jsonPath("$.balance").value(1000.00));

        verify(walletService).createWallet(any(CreateWalletRequest.class));
    }

    @Test
    void createWallet_whenWalletAlreadyExists_returnsConflict() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("1000.00"))
                .build();

        when(walletService.createWallet(any(CreateWalletRequest.class)))
                .thenThrow(WalletServiceException.walletAlreadyExists(1001L));

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.errorCode").value("WALLET_ALREADY_EXISTS"))
                .andExpect(jsonPath("$.message").value("Wallet already exists for userId: 1001"));
    }

    @Test
    void createWallet_missingUserId_returnsBadRequest() throws Exception {
        String body = """
                {"balance":1000.00}
                """;

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(body))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message").value("userId can not be empty"));

        verify(walletService, never()).createWallet(any());
    }

    @Test
    void createWallet_negativeBalance_returnsBadRequest() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("-1.00"))
                .build();

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message").value("Invalid balance. Balance must be at least 0.00"));

        verify(walletService, never()).createWallet(any());
    }

    @Test
    void createWallet_whenBalanceHasTooManyFractionDigits_returnsBadRequest() throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("100.001"))
                .build();

        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("balance must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"));

        verify(walletService, never()).createWallet(any());
    }
}
