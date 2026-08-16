package com.api.wallet.controller;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.api.wallet.TestcontainersConfiguration;
import com.api.wallet.domain.EntryType;
import com.api.wallet.domain.LedgerEntry;
import com.api.wallet.domain.Transfer;
import com.api.wallet.domain.Wallet;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.repository.LedgerEntryRepository;
import com.api.wallet.repository.TransferRepository;
import com.api.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.List;
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
class TransferApiIntegrationTest {

    @Autowired private MockMvc mockMvc;
    @Autowired private JsonMapper jsonMapper;
    @Autowired private WalletRepository walletRepository;
    @Autowired private TransferRepository transferRepository;
    @Autowired private LedgerEntryRepository ledgerEntryRepository;

    private String fromWalletId;
    private String toWalletId;

    @BeforeEach
    void setUp() {
        fromWalletId = "wallet-" + UUID.randomUUID();
        toWalletId = "wallet-" + UUID.randomUUID();
        walletRepository.save(new Wallet(fromWalletId, new BigDecimal("1000.00")));
        walletRepository.save(new Wallet(toWalletId, new BigDecimal("500.00")));
    }

    @Test
    void createsTransferAndRecordsBalancedLedger() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();
        String requestBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, new BigDecimal("100")));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status").value("PROCESSED"))
                .andExpect(jsonPath("$.fromWalletId").value(fromWalletId))
                .andExpect(jsonPath("$.toWalletId").value(toWalletId));

        assertThat(walletRepository.findById(fromWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo("900.00");
        assertThat(walletRepository.findById(toWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo("600.00");

        Transfer transfer = onlyTransferFor(idempotencyKey);
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transfer.getId());
        assertThat(entries).hasSize(2);
        assertThat(entries)
                .extracting(LedgerEntry::getEntryType)
                .containsExactlyInAnyOrder(EntryType.DEBIT, EntryType.CREDIT);
        assertThat(entries).allMatch(entry -> entry.getAmount().compareTo(new BigDecimal("100")) == 0);
    }

    @Test
    void sameIdempotencyKeyAlwaysReplaysTheOriginalResponse() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();
        String originalBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, new BigDecimal("100")));

        String firstResponse =
                mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(originalBody))
                        .andExpect(status().isCreated())
                        .andReturn()
                        .getResponse()
                        .getContentAsString();

        String sameBodyReplay =
                mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(originalBody))
                        .andExpect(status().isCreated())
                        .andReturn()
                        .getResponse()
                        .getContentAsString();
        assertThat(sameBodyReplay).isEqualTo(firstResponse);

        String differentBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, new BigDecimal("999")));
        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(differentBody))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.amount").value(100));

        assertThat(walletRepository.findById(fromWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo("900.00");

        Transfer transfer = onlyTransferFor(idempotencyKey);
        assertThat(ledgerEntryRepository.findByTransferId(transfer.getId())).hasSize(2);
    }

    @Test
    void insufficientFundsProducesFailedTransferWithoutLedgerEntries() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();
        String requestBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, new BigDecimal("5000")));

        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(requestBody))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status").value("FAILED"))
                .andExpect(jsonPath("$.failureReason").value("INSUFFICIENT_FUNDS"));

        assertThat(walletRepository.findById(fromWalletId).orElseThrow().getBalance())
                .isEqualByComparingTo("1000.00");

        Transfer transfer = onlyTransferFor(idempotencyKey);
        assertThat(ledgerEntryRepository.findByTransferId(transfer.getId())).isEmpty();
    }

    @Test
    void invalidTransferRequestsAreRejectedWithoutCreatingATransfer() throws Exception {
        String sameWalletBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(
                                UUID.randomUUID().toString(), fromWalletId, fromWalletId, new BigDecimal("10")));
        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(sameWalletBody))
                .andExpect(status().isBadRequest());

        String missingWalletKey = UUID.randomUUID().toString();
        String missingWalletBody =
                jsonMapper.writeValueAsString(
                        new CreateTransferRequest(missingWalletKey, fromWalletId, "no_such_wallet", new BigDecimal("10")));
        mockMvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(missingWalletBody))
                .andExpect(status().isNotFound());

        assertThat(transferRepository.findAll()).noneMatch(
                transfer -> transfer.getIdempotencyKey().equals(missingWalletKey));
    }

    private Transfer onlyTransferFor(String idempotencyKey) {
        List<Transfer> matches =
                transferRepository.findAll().stream()
                        .filter(transfer -> transfer.getIdempotencyKey().equals(idempotencyKey))
                        .toList();
        assertThat(matches).hasSize(1);
        return matches.get(0);
    }
}
