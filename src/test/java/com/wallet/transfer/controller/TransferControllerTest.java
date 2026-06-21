package com.wallet.transfer.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.BaseIntegrationTest;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.web.servlet.MockMvc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.hamcrest.Matchers.is;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

@AutoConfigureMockMvc
@DisplayName("Transfer API")
class TransferControllerTest extends BaseIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private JdbcTemplate jdbcTemplate;

    @BeforeEach
    void setUp() {
        jdbcTemplate.execute("TRUNCATE TABLE ledger_entries, transfers, wallets CASCADE");

        walletRepository.save(new Wallet("wallet_1", 1000L));
        walletRepository.save(new Wallet("wallet_2", 500L));
    }

    @Test
    @DisplayName("POST /transfers should create transfer and return 201 with Location header")
    void createTransfer() throws Exception {
        TransferRequest request = new TransferRequest("api-key-1", "wallet_1", "wallet_2", 250L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.fromWalletId", is("wallet_1")))
                .andExpect(jsonPath("$.toWalletId", is("wallet_2")))
                .andExpect(jsonPath("$.amount", is(250)))
                .andExpect(jsonPath("$.status", is("PROCESSED")))
                .andExpect(result -> {
                    String location = result.getResponse().getHeader("Location");
                    assertThat(location).isNotNull();
                    assertThat(location).contains("/transfers/");
                });
    }

    @Test
    @DisplayName("POST /transfers with duplicate key should return 200 (not 201)")
    void duplicateTransfer() throws Exception {
        TransferRequest request = new TransferRequest("dup-key", "wallet_1", "wallet_2", 100L);
        String body = objectMapper.writeValueAsString(request);

        String firstResponse = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(body))
                .andExpect(status().isCreated())
                .andReturn().getResponse().getContentAsString();

        String secondResponse = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(body))
                .andExpect(status().isOk())
                .andReturn().getResponse().getContentAsString();

        assertResponsesHaveSameId(firstResponse, secondResponse);
    }

    @Test
    @DisplayName("POST /transfers with insufficient funds should return 422")
    void insufficientFunds() throws Exception {
        TransferRequest request = new TransferRequest("fail-key", "wallet_1", "wallet_2", 5000L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.error", is("INSUFFICIENT_FUNDS")));
    }

    @Test
    @DisplayName("POST /transfers replay of FAILED transfer should return 422 again")
    void failedTransferReplay() throws Exception {
        TransferRequest request = new TransferRequest("fail-replay", "wallet_1", "wallet_2", 5000L);
        String body = objectMapper.writeValueAsString(request);

        // First attempt fails
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(body))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.error", is("INSUFFICIENT_FUNDS")));

        // Retry with same idempotency key should also return 422
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(body))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.error", is("INSUFFICIENT_FUNDS")));
    }

    @Test
    @DisplayName("POST /transfers with same key but different payload should return 409")
    void idempotencyKeyConflict() throws Exception {
        TransferRequest first = new TransferRequest("conflict-key", "wallet_1", "wallet_2", 100L);
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(first)))
                .andExpect(status().isCreated());

        // Same key, different amount
        TransferRequest second = new TransferRequest("conflict-key", "wallet_1", "wallet_2", 200L);
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(second)))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.error", is("IDEMPOTENCY_KEY_CONFLICT")));
    }

    @Test
    @DisplayName("POST /transfers with missing fields should return 400")
    void validationError() throws Exception {
        String invalidBody = "{\"amount\": 100}";

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(invalidBody))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("VALIDATION_ERROR")));
    }

    @Test
    @DisplayName("POST /transfers to same wallet should return 400")
    void sameWalletTransfer() throws Exception {
        TransferRequest request = new TransferRequest("same-wallet", "wallet_1", "wallet_1", 100L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("BAD_REQUEST")));
    }

    @Test
    @DisplayName("POST /transfers with nonexistent wallet should return 404")
    void walletNotFound() throws Exception {
        TransferRequest request = new TransferRequest("nf-key", "ghost", "wallet_2", 100L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.error", is("WALLET_NOT_FOUND")));
    }

    @Test
    @DisplayName("POST /transfers with zero amount should return 400")
    void zeroAmount() throws Exception {
        TransferRequest request = new TransferRequest("zero-key", "wallet_1", "wallet_2", 0L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("VALIDATION_ERROR")));
    }

    @Test
    @DisplayName("POST /transfers with negative amount should return 400")
    void negativeAmount() throws Exception {
        TransferRequest request = new TransferRequest("neg-key", "wallet_1", "wallet_2", -50L);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("VALIDATION_ERROR")));
    }

    @Test
    @DisplayName("GET /transfers/{id} should return transfer details")
    void getTransfer() throws Exception {
        TransferRequest request = new TransferRequest("get-key", "wallet_1", "wallet_2", 150L);
        String createResponse = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andReturn().getResponse().getContentAsString();

        String transferId = objectMapper.readTree(createResponse).get("id").asText();

        mockMvc.perform(get("/transfers/" + transferId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.id", is(transferId)))
                .andExpect(jsonPath("$.fromWalletId", is("wallet_1")))
                .andExpect(jsonPath("$.toWalletId", is("wallet_2")))
                .andExpect(jsonPath("$.amount", is(150)))
                .andExpect(jsonPath("$.status", is("PROCESSED")));
    }

    @Test
    @DisplayName("GET /transfers/{id} with unknown id should return 404")
    void getTransferNotFound() throws Exception {
        mockMvc.perform(get("/transfers/nonexistent"))
                .andExpect(status().isNotFound());
    }

    @Test
    @DisplayName("GET /wallets/{id} should return wallet balance")
    void getWallet() throws Exception {
        mockMvc.perform(get("/wallets/wallet_1"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.id", is("wallet_1")))
                .andExpect(jsonPath("$.balance", is(1000)));
    }

    @Test
    @DisplayName("GET /wallets/{id} with unknown id should return 404")
    void getWalletNotFound() throws Exception {
        mockMvc.perform(get("/wallets/unknown"))
                .andExpect(status().isNotFound());
    }

    private void assertResponsesHaveSameId(String first, String second) throws Exception {
        var firstNode = objectMapper.readTree(first);
        var secondNode = objectMapper.readTree(second);
        assertThat(firstNode.get("id").asText()).isEqualTo(secondNode.get("id").asText());
    }
}
