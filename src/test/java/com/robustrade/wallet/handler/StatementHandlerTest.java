package com.robustrade.wallet.handler;

import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.robustrade.wallet.domain.TransactionType;
import com.robustrade.wallet.handler.dto.StatementEntryResponse;
import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.service.StatementService;
import com.robustrade.wallet.service.WalletServiceException;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.test.web.servlet.MockMvc;

@WebMvcTest(controllers = StatementHandler.class)
@Import(GlobalExceptionHandler.class)
class StatementHandlerTest {

    @Autowired
    private MockMvc mockMvc;

    @MockBean
    private StatementService statementService;

    @Test
    void getStatement_byWalletId_returnsOk() throws Exception {
        UUID transferId = UUID.randomUUID();
        StatementResponse response = StatementResponse.builder()
                .walletId("wallet-1")
                .userId(1001L)
                .currentBalance(new BigDecimal("900.00"))
                .entries(List.of(
                        StatementEntryResponse.builder()
                                .transferId(transferId)
                                .transferType(TransactionType.DEBIT)
                                .amount(new BigDecimal("100.00"))
                                .previousBalance(new BigDecimal("1000.00"))
                                .balanceAfterTransfer(new BigDecimal("900.00"))
                                .transferDate(Instant.parse("2026-09-13T11:00:00Z"))
                                .build()
                ))
                .build();

        when(statementService.getStatement(isNull(), eq("wallet-1"))).thenReturn(response);

        mockMvc.perform(get("/statements").param("walletId", "wallet-1"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.walletId").value("wallet-1"))
                .andExpect(jsonPath("$.userId").value(1001))
                .andExpect(jsonPath("$.currentBalance").value(900.00))
                .andExpect(jsonPath("$.entries[0].transferType").value("DEBIT"))
                .andExpect(jsonPath("$.entries[0].amount").value(100.00));

        verify(statementService).getStatement(isNull(), eq("wallet-1"));
    }

    @Test
    void getStatement_byUserId_returnsOk() throws Exception {
        StatementResponse response = StatementResponse.builder()
                .walletId("wallet-1")
                .userId(1001L)
                .currentBalance(new BigDecimal("500.00"))
                .entries(List.of())
                .build();

        when(statementService.getStatement(eq(1001L), isNull())).thenReturn(response);

        mockMvc.perform(get("/statements").param("userId", "1001"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.walletId").value("wallet-1"))
                .andExpect(jsonPath("$.userId").value(1001))
                .andExpect(jsonPath("$.entries").isEmpty());

        verify(statementService).getStatement(eq(1001L), isNull());
    }

    @Test
    void getStatement_whenMissingLookup_returnsBadRequest() throws Exception {
        when(statementService.getStatement(isNull(), isNull()))
                .thenThrow(WalletServiceException.missingLookup());

        mockMvc.perform(get("/statements"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"));

        verify(statementService).getStatement(isNull(), isNull());
    }

    @Test
    void getStatement_whenWalletNotFound_returnsNotFound() throws Exception {
        when(statementService.getStatement(isNull(), eq("missing")))
                .thenThrow(WalletServiceException.walletNotFound());

        mockMvc.perform(get("/statements").param("walletId", "missing"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.errorCode").value("WALLET_NOT_FOUND"));

        verify(statementService).getStatement(isNull(), eq("missing"));
    }

    @Test
    void getStatement_whenMultipleWallets_returnsBadRequest() throws Exception {
        when(statementService.getStatement(eq(1001L), isNull()))
                .thenThrow(WalletServiceException.multipleWalletsForUser());

        mockMvc.perform(get("/statements").param("userId", "1001"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("MULTIPLE_WALLETS"));

        verify(statementService).getStatement(eq(1001L), isNull());
        verify(statementService, never()).getStatement(eq(1001L), eq("wallet-1"));
    }
}
