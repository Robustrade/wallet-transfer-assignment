package com.wallettransfer;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallettransfer.domain.TransferStatus;
import com.wallettransfer.repository.IdempotencyRecordRepository;
import com.wallettransfer.repository.LedgerEntryRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.service.TransferService;
import com.wallettransfer.service.WalletService;
import com.wallettransfer.web.dto.CreateTransferRequest;
import com.wallettransfer.web.dto.CreateWalletRequest;
import com.wallettransfer.web.dto.TransferResponse;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;

/**
 * End-to-end tests for transfer API, idempotency, ledger, and concurrent debits.
 * Uses in-memory H2 with the {@code test} Spring profile.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
class TransferIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletService walletService;

    /** Clears tables between tests (respects FK order: idempotency before transfers). */
    @BeforeEach
    void cleanDatabase() {
        ledgerEntryRepository.deleteAll();
        idempotencyRecordRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();
    }

    /** Happy path: balances update and exactly two ledger rows (DEBIT + CREDIT) are written. */
    @Test
    void transfer_movesFundsAndWritesBalancedLedger() throws Exception {
        createWallet("wallet_1", 500);
        createWallet("wallet_2", 0);

        MvcResult result = createTransfer(new CreateTransferRequest("key-1", "wallet_1", "wallet_2", 100));

        JsonNode body = objectMapper.readTree(result.getResponse().getContentAsString());
        assertThat(body.get("status").asText()).isEqualTo("PROCESSED");
        assertThat(body.get("ledgerEntries")).hasSize(2);
        assertThat(body.get("ledgerEntries").get(0).get("type").asText()).isEqualTo("CREDIT");
        assertThat(body.get("ledgerEntries").get(1).get("type").asText()).isEqualTo("DEBIT");

        mockMvc.perform(get("/wallets/wallet_1"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.balance").value(400));
        mockMvc.perform(get("/wallets/wallet_2"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.balance").value(100));
    }

    /** Replaying the same idempotency key must not create a second transfer or debit twice. */
    @Test
    void duplicateIdempotencyKeyReturnsSameTransferWithoutDoubleSpend() throws Exception {
        createWallet("wallet_1", 300);
        createWallet("wallet_2", 0);

        MvcResult first = createTransfer(new CreateTransferRequest("dup-key", "wallet_1", "wallet_2", 50));
        MvcResult second = createTransfer(new CreateTransferRequest("dup-key", "wallet_1", "wallet_2", 50));

        JsonNode firstBody = objectMapper.readTree(first.getResponse().getContentAsString());
        JsonNode secondBody = objectMapper.readTree(second.getResponse().getContentAsString());

        assertThat(secondBody.get("id").asText()).isEqualTo(firstBody.get("id").asText());
        assertThat(transferRepository.count()).isEqualTo(1);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);

        mockMvc.perform(get("/wallets/wallet_1"))
                .andExpect(jsonPath("$.balance").value(250));
    }

    /** Same key with different amount must return HTTP 409 Conflict. */
    @Test
    void idempotencyKeyWithDifferentPayloadIsRejected() throws Exception {
        createWallet("wallet_1", 200);
        createWallet("wallet_2", 0);

        createTransfer(new CreateTransferRequest("same-key", "wallet_1", "wallet_2", 10));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(
                                new CreateTransferRequest("same-key", "wallet_1", "wallet_2", 20))))
                .andExpect(status().isConflict());
    }

    /** Failed transfers must not write ledger rows or change balances. */
    @Test
    void insufficientFundsMarksTransferFailedWithoutLedger() throws Exception {
        createWallet("wallet_1", 30);
        createWallet("wallet_2", 0);

        MvcResult result = createTransfer(new CreateTransferRequest("fail-key", "wallet_1", "wallet_2", 100));

        JsonNode body = objectMapper.readTree(result.getResponse().getContentAsString());
        assertThat(body.get("status").asText()).isEqualTo("FAILED");
        assertThat(body.get("ledgerEntries")).isEmpty();
        assertThat(ledgerEntryRepository.count()).isZero();

        mockMvc.perform(get("/wallets/wallet_1"))
                .andExpect(jsonPath("$.balance").value(30));
    }

    /**
     * Ten parallel 30-unit debits from a 100 balance: exactly 3 succeed, 7 fail, 10 left on source.
     */
    @Test
    void concurrentDebitsDoNotOverdrawWallet() throws Exception {
        walletService.createWallet(new CreateWalletRequest("wallet_1", 100));
        walletService.createWallet(new CreateWalletRequest("wallet_2", 0));
        walletService.createWallet(new CreateWalletRequest("wallet_3", 0));

        ExecutorService executor = Executors.newFixedThreadPool(8);
        try {
            List<Callable<TransferResponse>> tasks = new ArrayList<>();
            for (int i = 0; i < 10; i++) {
                String key = "concurrent-" + i;
                String toWallet = i % 2 == 0 ? "wallet_2" : "wallet_3";
                tasks.add(() -> transferService.createTransfer(
                        new CreateTransferRequest(key, "wallet_1", toWallet, 30)));
            }

            List<Future<TransferResponse>> results = executor.invokeAll(tasks);
            long processed = results.stream()
                    .map(f -> readStatus(f))
                    .filter(TransferStatus.PROCESSED::equals)
                    .count();
            long failed = results.stream()
                    .map(f -> readStatus(f))
                    .filter(TransferStatus.FAILED::equals)
                    .count();

            assertThat(processed).isEqualTo(3);
            assertThat(failed).isEqualTo(7);
            assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance())
                    .isEqualTo(10);
        } finally {
            executor.shutdownNow();
        }
    }

    /** Unwraps transfer status from a concurrent task result. */
    private TransferStatus readStatus(Future<TransferResponse> future) {
        try {
            return future.get().status();
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    /** Helper: POST /wallets via MockMvc. */
    private void createWallet(String id, long balance) throws Exception {
        mockMvc.perform(post("/wallets")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(new CreateWalletRequest(id, balance))))
                .andExpect(status().isCreated());
    }

    /** Helper: POST /transfers via MockMvc. */
    private MvcResult createTransfer(CreateTransferRequest request) throws Exception {
        return mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andReturn();
    }
}
