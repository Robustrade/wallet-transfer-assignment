package com.robustrade.wallet.integration;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.handler.dto.WalletResponse;
import java.math.BigDecimal;
import org.junit.jupiter.api.BeforeEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;

@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
abstract class AbstractIntegrationTest {

    @Autowired
    protected MockMvc mockMvc;

    @Autowired
    protected ObjectMapper objectMapper;

    @Autowired
    protected JdbcTemplate jdbcTemplate;

    @BeforeEach
    void cleanDatabase() {
        jdbcTemplate.update("DELETE FROM ledger_entries");
        jdbcTemplate.update("DELETE FROM idempotency_records");
        jdbcTemplate.update("DELETE FROM transfers");
        jdbcTemplate.update("DELETE FROM wallets");
    }

    protected WalletResponse createWallet(long userId, String balance) throws Exception {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(userId)
                .balance(new BigDecimal(balance))
                .build();

        MvcResult result = mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andReturn();

        return objectMapper.readValue(result.getResponse().getContentAsString(), WalletResponse.class);
    }

    protected TransferResponse createTransfer(
            String idempotencyKey,
            String fromWalletId,
            String toWalletId,
            String amount
    ) throws Exception {
        MvcResult result = performTransfer(idempotencyKey, fromWalletId, toWalletId, amount)
                .andExpect(status().isCreated())
                .andReturn();
        return objectMapper.readValue(result.getResponse().getContentAsString(), TransferResponse.class);
    }

    protected org.springframework.test.web.servlet.ResultActions performTransfer(
            String idempotencyKey,
            String fromWalletId,
            String toWalletId,
            String amount
    ) throws Exception {
        CreateTransferRequest request = CreateTransferRequest.builder()
                .idempotencyKey(idempotencyKey)
                .fromWalletId(fromWalletId)
                .toWalletId(toWalletId)
                .amount(new BigDecimal(amount))
                .build();

        return mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)));
    }

    protected StatementResponse getStatementByWalletId(String walletId) throws Exception {
        MvcResult result = mockMvc.perform(get("/statements").param("walletId", walletId))
                .andExpect(status().isOk())
                .andReturn();
        return objectMapper.readValue(result.getResponse().getContentAsString(), StatementResponse.class);
    }

    protected StatementResponse getStatementByUserId(long userId) throws Exception {
        MvcResult result = mockMvc.perform(get("/statements").param("userId", String.valueOf(userId)))
                .andExpect(status().isOk())
                .andReturn();
        return objectMapper.readValue(result.getResponse().getContentAsString(), StatementResponse.class);
    }

    protected String errorCode(MvcResult result) throws Exception {
        JsonNode node = objectMapper.readTree(result.getResponse().getContentAsString());
        return node.path("errorCode").asText();
    }

    protected BigDecimal walletBalance(String walletId) {
        return jdbcTemplate.queryForObject(
                "SELECT balance FROM wallets WHERE id = ?",
                BigDecimal.class,
                walletId
        );
    }

    protected String transferState(java.util.UUID transferId) {
        return jdbcTemplate.queryForObject(
                "SELECT state FROM transfers WHERE id = ?",
                String.class,
                transferId
        );
    }

    protected int ledgerEntryCount(java.util.UUID transferId) {
        Integer count = jdbcTemplate.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?",
                Integer.class,
                transferId
        );
        return count == null ? 0 : count;
    }
}
