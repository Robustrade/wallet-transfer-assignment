package com.wallet.transferservice;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transferservice.dto.ErrorResponse;
import com.wallet.transferservice.dto.TransferRequest;
import com.wallet.transferservice.dto.TransferResponse;
import com.wallet.transferservice.domain.Wallet;
import com.wallet.transferservice.domain.enums.WalletStatus;
import com.wallet.transferservice.repository.LedgerEntryRepository;
import com.wallet.transferservice.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.*;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
class TransferServiceIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    private Wallet sourceWallet;
    private Wallet destWallet;

    @BeforeEach
    void setUp() {
        // Create fresh wallets for each test
        sourceWallet = new Wallet(UUID.randomUUID(), new BigDecimal("1000.00"));
        destWallet = new Wallet(UUID.randomUUID(), new BigDecimal("500.00"));
        walletRepository.save(sourceWallet);
        walletRepository.save(destWallet);
    }

    // ── Happy Path ────────────────────────────────────────────

    @Test
    @DisplayName("1. Valid transfer succeeds with 201, balances updated, 2 ledger entries created")
    void happyPath_transferSucceeds() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();

        MvcResult result = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                idempotencyKey,
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "100.50"
                        ))))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status").value("COMPLETED"))
                .andExpect(jsonPath("$.amount").value("100.50"))
                .andReturn();

        TransferResponse response = fromJson(result, TransferResponse.class);

        // Verify balances
        Wallet updatedSource = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        Wallet updatedDest = walletRepository.findById(destWallet.getId()).orElseThrow();
        assertThat(updatedSource.getBalance()).isEqualByComparingTo("899.50");
        assertThat(updatedDest.getBalance()).isEqualByComparingTo("600.50");

        // Verify ledger entries
        var entries = ledgerEntryRepository.findByTransferId(UUID.fromString(response.transferId()));
        assertThat(entries).hasSize(2);
    }

    @Test
    @DisplayName("10. Exact balance transfer succeeds, source balance becomes 0")
    void exactBalanceTransfer_succeeds() throws Exception {
        // Set source balance to exactly 50.00 (initial is 1000.00)
        sourceWallet.debit(new BigDecimal("950.00"));
        walletRepository.save(sourceWallet);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                UUID.randomUUID().toString(),
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "50.00"
                        ))))
                .andExpect(status().isCreated());

        Wallet updatedSource = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        assertThat(updatedSource.getBalance()).isEqualByComparingTo("0.00");
    }

    // ── Validation ────────────────────────────────────────────

    @Test
    @DisplayName("5. Self-transfer returns 400 SELF_TRANSFER")
    void selfTransfer_returns400() throws Exception {
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                UUID.randomUUID().toString(),
                                sourceWallet.getId().toString(),
                                sourceWallet.getId().toString(),
                                "10.00"
                        ))))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.code").value("SELF_TRANSFER"));
    }

    @Test
    @DisplayName("6. Non-existent source wallet returns 404")
    void invalidSourceWallet_returns404() throws Exception {
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                UUID.randomUUID().toString(),
                                UUID.randomUUID().toString(),  // non-existent
                                destWallet.getId().toString(),
                                "10.00"
                        ))))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.code").value("WALLET_NOT_FOUND"));
    }

    @Test
    @DisplayName("9. Insufficient funds returns 409 INSUFFICIENT_FUNDS")
    void insufficientFunds_returns409() throws Exception {
        sourceWallet.debit(new BigDecimal("950.00")); // Leaves 50.00
        walletRepository.save(sourceWallet);

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                UUID.randomUUID().toString(),
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "50.01"
                        ))))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.code").value("INSUFFICIENT_FUNDS"));
    }

    // ── Idempotency ───────────────────────────────────────────

    @Test
    @DisplayName("12. Duplicate request with same params returns 200 with same transferId")
    void idempotentReplay_returns200() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();
        TransferRequest request = new TransferRequest(
                idempotencyKey,
                sourceWallet.getId().toString(),
                destWallet.getId().toString(),
                "100.00"
        );

        // First request → 201
        MvcResult first = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(request)))
                .andExpect(status().isCreated())
                .andReturn();

        // Second request (same key, same params) → 200
        MvcResult second = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(request)))
                .andExpect(status().isOk())
                .andReturn();

        TransferResponse firstResp = fromJson(first, TransferResponse.class);
        TransferResponse secondResp = fromJson(second, TransferResponse.class);

        // Same transferId
        assertThat(secondResp.transferId()).isEqualTo(firstResp.transferId());

        // Balance should only reflect one transfer
        Wallet updatedSource = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        assertThat(updatedSource.getBalance()).isEqualByComparingTo("900.00");
    }

    @Test
    @DisplayName("13. Duplicate key with different amount returns 409 IDEMPOTENCY_KEY_CONFLICT")
    void idempotencyKeyConflict_differentAmount() throws Exception {
        String idempotencyKey = UUID.randomUUID().toString();

        // First request → 201
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                idempotencyKey,
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "100.00"
                        ))))
                .andExpect(status().isCreated());

        // Second request with different amount → 409
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                idempotencyKey,
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "200.00"  // different amount
                        ))))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.code").value("IDEMPOTENCY_KEY_CONFLICT"));
    }

    // ── Concurrency ───────────────────────────────────────────

    @Test
    @DisplayName("17. Concurrent double-spend: only one succeeds, total balance conserved")
    void concurrentDoubleSpend_onlyOneSucceeds() throws Exception {
        // Source has $100, two concurrent requests each try to take $80
        sourceWallet.debit(new BigDecimal("900.00")); // Leaves 100.00
        walletRepository.save(sourceWallet);

        Wallet otherDest = new Wallet(UUID.randomUUID(), BigDecimal.ZERO);
        walletRepository.save(otherDest);

        int threadCount = 2;
        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch latch = new CountDownLatch(1);
        List<Future<Integer>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            UUID dest = (i == 0) ? destWallet.getId() : otherDest.getId();
            futures.add(executor.submit(() -> {
                latch.await(); // Synchronize start
                MvcResult r = mockMvc.perform(post("/transfers")
                                .contentType(MediaType.APPLICATION_JSON)
                                .content(toJson(new TransferRequest(
                                        UUID.randomUUID().toString(),
                                        sourceWallet.getId().toString(),
                                        dest.toString(),
                                        "80.00"
                                ))))
                        .andReturn();
                return r.getResponse().getStatus();
            }));
        }

        latch.countDown(); // Release both threads simultaneously
        executor.shutdown();
        executor.awaitTermination(10, TimeUnit.SECONDS);

        List<Integer> statuses = new ArrayList<>();
        for (Future<Integer> f : futures) {
            statuses.add(f.get());
        }

        // Exactly one 201, one 409
        assertThat(statuses).containsExactlyInAnyOrder(201, 409);

        // Source balance should be 20 (100 - 80), not negative
        Wallet updatedSource = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        assertThat(updatedSource.getBalance()).isEqualByComparingTo("20.00");
    }

    // ── Consistency ───────────────────────────────────────────

    @Test
    @DisplayName("22. SUM of all ledger entries equals zero (double-entry invariant)")
    void ledgerSumIsZero() throws Exception {
        // Execute a few transfers
        for (int i = 0; i < 3; i++) {
            mockMvc.perform(post("/transfers")
                            .contentType(MediaType.APPLICATION_JSON)
                            .content(toJson(new TransferRequest(
                                    UUID.randomUUID().toString(),
                                    sourceWallet.getId().toString(),
                                    destWallet.getId().toString(),
                                    "10.00"
                            ))))
                    .andExpect(status().isCreated());
        }

        BigDecimal totalSum = ledgerEntryRepository.sumAllAmounts();
        assertThat(totalSum).isEqualByComparingTo("0.00");
    }

    @Test
    @DisplayName("23. Wallet balance matches SUM of its ledger entries")
    void walletBalanceMatchesLedger() throws Exception {
        // Start with known balance
        BigDecimal initialSourceBalance = sourceWallet.getBalance();
        BigDecimal initialDestBalance = destWallet.getBalance();

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(toJson(new TransferRequest(
                                UUID.randomUUID().toString(),
                                sourceWallet.getId().toString(),
                                destWallet.getId().toString(),
                                "150.00"
                        ))))
                .andExpect(status().isCreated());

        // The materialized balance should equal initial + SUM(ledger)
        BigDecimal sourceLedgerSum = ledgerEntryRepository.sumAmountsByWalletId(sourceWallet.getId());
        BigDecimal destLedgerSum = ledgerEntryRepository.sumAmountsByWalletId(destWallet.getId());

        Wallet updatedSource = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        Wallet updatedDest = walletRepository.findById(destWallet.getId()).orElseThrow();

        assertThat(updatedSource.getBalance()).isEqualByComparingTo(initialSourceBalance.add(sourceLedgerSum));
        assertThat(updatedDest.getBalance()).isEqualByComparingTo(initialDestBalance.add(destLedgerSum));
    }

    // ── Helpers ───────────────────────────────────────────────

    private String toJson(Object obj) throws Exception {
        return objectMapper.writeValueAsString(obj);
    }

    private <T> T fromJson(MvcResult result, Class<T> clazz) throws Exception {
        return objectMapper.readValue(result.getResponse().getContentAsString(), clazz);
    }
}
