package com.example.wallet.web;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.example.wallet.repository.IdempotencyRecordRepository;
import com.example.wallet.repository.LedgerEntryRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.support.TestWalletFactory;
import java.math.BigDecimal;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;
import org.springframework.test.web.servlet.setup.MockMvcBuilders;
import org.springframework.web.context.WebApplicationContext;

@SpringBootTest
class TransferControllerTest {

    @Autowired
    private WebApplicationContext webApplicationContext;

    @Autowired
    private TestWalletFactory testWalletFactory;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Autowired
    private ObjectMapper objectMapper;

    private MockMvc mockMvc;

    @BeforeEach
    void setUp() {
        mockMvc = MockMvcBuilders.webAppContextSetup(webApplicationContext).build();
        ledgerEntryRepository.deleteAll();
        idempotencyRecordRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();

        testWalletFactory.createWallet("wallet_1", new BigDecimal("500.0000"));
        testWalletFactory.createWallet("wallet_2", new BigDecimal("100.0000"));
    }

    @Test
    void createsTransferWithDoubleEntryLedger() throws Exception {
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content("""
                                {
                                  "idempotencyKey": "transfer-1",
                                  "fromWalletId": "wallet_1",
                                  "toWalletId": "wallet_2",
                                  "amount": 100
                                }
                                """))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status").value("PROCESSED"))
                .andExpect(jsonPath("$.amount").value(100));

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance())
                .isEqualByComparingTo(new BigDecimal("400.0000"));
        assertThat(walletRepository.findById("wallet_2").orElseThrow().getBalance())
                .isEqualByComparingTo(new BigDecimal("200.0000"));
        assertThat(transferRepository.count()).isEqualTo(1);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    }

    @Test
    void replaysIdempotentRequestWithoutDuplicatingSideEffects() throws Exception {
        String payload = """
                {
                  "idempotencyKey": "duplicate-key",
                  "fromWalletId": "wallet_1",
                  "toWalletId": "wallet_2",
                  "amount": 50
                }
                """;

        MvcResult firstResponse = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(payload))
                .andExpect(status().isCreated())
                .andReturn();

        JsonNode firstBody = objectMapper.readTree(firstResponse.getResponse().getContentAsString());

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(payload))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.transferId").value(firstBody.get("transferId").asText()));

        assertThat(transferRepository.count()).isEqualTo(1);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);
        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance())
                .isEqualByComparingTo(new BigDecimal("450.0000"));
    }

    @Test
    void failsWhenSourceWalletHasInsufficientFunds() throws Exception {
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content("""
                                {
                                  "idempotencyKey": "insufficient-funds",
                                  "fromWalletId": "wallet_1",
                                  "toWalletId": "wallet_2",
                                  "amount": 1000
                                }
                                """))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.status").value("FAILED"))
                .andExpect(jsonPath("$.errorMessage").value("insufficient funds"));

        assertThat(ledgerEntryRepository.count()).isZero();
        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance())
                .isEqualByComparingTo(new BigDecimal("500.0000"));
    }

    @Test
    void rejectsTransferToSameWallet() throws Exception {
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content("""
                                {
                                  "idempotencyKey": "same-wallet",
                                  "fromWalletId": "wallet_1",
                                  "toWalletId": "wallet_1",
                                  "amount": 10
                                }
                                """))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.status").value("FAILED"));

        assertThat(ledgerEntryRepository.count()).isZero();
    }
}
