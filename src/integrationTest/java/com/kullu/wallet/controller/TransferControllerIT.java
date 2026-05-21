package com.kullu.wallet.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.content;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import java.util.List;
import java.util.UUID;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kullu.wallet.dto.request.CreateTransferRequest;
import com.kullu.wallet.entity.EntryType;
import com.kullu.wallet.entity.LedgerEntry;
import com.kullu.wallet.repository.IdempotencyRecordRepository;
import com.kullu.wallet.repository.LedgerEntryRepository;
import com.kullu.wallet.repository.TransferRepository;
import com.kullu.wallet.repository.WalletRepository;
import com.kullu.wallet.support.AbstractPostgresIntegrationTest;

@AutoConfigureMockMvc
class TransferControllerIT extends AbstractPostgresIntegrationTest {

    @Autowired
    private MockMvc mockMvc;
    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepo;
    @Autowired
    private TransferRepository transferRepo;
    @Autowired
    private LedgerEntryRepository ledgerRepo;
    @Autowired
    private IdempotencyRecordRepository idempotencyRepo;

    private String from;
    private String to;

    @BeforeEach
    void seed() {
        // Clean slate per test.
        ledgerRepo.deleteAllInBatch();
        idempotencyRepo.deleteAllInBatch();
        transferRepo.deleteAllInBatch();
        walletRepo.deleteAllInBatch();

        from = seedWallet(walletRepo, 1_000);
        to = seedWallet(walletRepo, 500);
    }

    @Test
    void happyPathCreatesTransferAndUpdatesBalances() throws Exception {
        String key = "k-happy";
        MvcResult res = mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest(key, from, to, 200L))))
            .andExpect(status().isCreated())
            .andExpect(content().contentTypeCompatibleWith(MediaType.APPLICATION_JSON))
            .andExpect(jsonPath("$.status").value("PROCESSED"))
            .andExpect(jsonPath("$.amount").value(200))
            .andReturn();

        JsonNode body = objectMapper.readTree(res.getResponse().getContentAsString());
        UUID transferId = UUID.fromString(body.get("transferId").asText());

        assertThat(walletRepo.findById(from).orElseThrow().getBalance()).isEqualTo(800);
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(700);

        List<LedgerEntry> entries = ledgerRepo.findAllByTransferId(transferId);
        assertThat(entries).hasSize(2);
        assertThat(entries.stream().mapToLong(LedgerEntry::signedAmount).sum())
            .as("ledger must sum to zero per transfer")
            .isZero();
        assertThat(entries).extracting(LedgerEntry::getType)
            .containsExactlyInAnyOrder(EntryType.DEBIT, EntryType.CREDIT);
    }

    @Test
    void replaySameKeySamePayloadReturns200WithIdenticalBody() throws Exception {
        String key = "k-replay";
        CreateTransferRequest req = new CreateTransferRequest(key, from, to, 150L);

        MvcResult first = mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(req)))
            .andExpect(status().isCreated())
            .andReturn();

        MvcResult second = mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(req)))
            .andExpect(status().isOk())
            .andReturn();

        assertThat(second.getResponse().getContentAsString())
            .isEqualTo(first.getResponse().getContentAsString());

        // No additional rows.
        assertThat(transferRepo.count()).isEqualTo(1);
        assertThat(ledgerRepo.count()).isEqualTo(2);

        assertThat(walletRepo.findById(from).orElseThrow().getBalance()).isEqualTo(850);
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(650);
    }

    @Test
    void replaySameKeyDifferentPayloadReturns409Conflict() throws Exception {
        String key = "k-conflict";
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest(key, from, to, 100L))))
            .andExpect(status().isCreated());

        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest(key, from, to, 200L))))
            .andExpect(status().isConflict())
            .andExpect(jsonPath("$.error").value("IDEMPOTENCY_CONFLICT"));
    }

    @Test
    void insufficientFundsPersistsFailedAndReturns422() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest("k-poor", from, to, 5_000L))))
            .andExpect(status().isUnprocessableEntity())
            .andExpect(jsonPath("$.status").value("FAILED"))
            .andExpect(jsonPath("$.failureReason").value("INSUFFICIENT_FUNDS"));

        // Balances unchanged.
        assertThat(walletRepo.findById(from).orElseThrow().getBalance()).isEqualTo(1_000);
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(500);

        // Ledger empty.
        assertThat(ledgerRepo.count()).isZero();

        // Idempotency record cached the FAILED outcome.
        assertThat(idempotencyRepo.findByKey("k-poor")).hasValueSatisfying(r ->
            assertThat(r.hasResponse()).isTrue());
    }

    @Test
    void insufficientFundsIsReplayable() throws Exception {
        CreateTransferRequest req = new CreateTransferRequest("k-poor-replay", from, to, 5_000L);

        MvcResult first = mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(req)))
            .andExpect(status().isUnprocessableEntity())
            .andReturn();

        MvcResult second = mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(req)))
            .andExpect(status().isOk())
            .andReturn();

        assertThat(second.getResponse().getContentAsString())
            .isEqualTo(first.getResponse().getContentAsString());
    }

    @Test
    void selfTransferReturns400() throws Exception {
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest("k-self", from, from, 100L))))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.error").value("SELF_TRANSFER"));
    }

    @Test
    void walletNotFoundReturns404() throws Exception {
        String missing = "missing-wallet";
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(json(new CreateTransferRequest("k-miss", missing, to, 100L))))
            .andExpect(status().isNotFound())
            .andExpect(jsonPath("$.error").value("WALLET_NOT_FOUND"));
    }

    @Test
    void validationErrorsReturns400() throws Exception {
        // negative amount
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"idempotencyKey\":\"k\",\"fromWalletId\":\""
                    + from + "\",\"toWalletId\":\"" + to + "\",\"amount\":-1}"))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.error").value("VALIDATION_ERROR"));

        // blank key
        mockMvc.perform(post("/transfers")
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"idempotencyKey\":\"\",\"fromWalletId\":\""
                    + from + "\",\"toWalletId\":\"" + to + "\",\"amount\":1}"))
            .andExpect(status().isBadRequest());
    }

    private String json(Object o) throws Exception {
        return objectMapper.writeValueAsString(o);
    }
}
