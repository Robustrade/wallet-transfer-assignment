package com.wallettransfer;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallettransfer.dto.requestdto.CreateTransferRequestDTO;
import com.wallettransfer.dto.requestdto.TransferHistoryRequestDTO;
import com.wallettransfer.dto.requestdto.WalletBalanceRequestDTO;
import com.wallettransfer.entity.Wallet;
import com.wallettransfer.repo.IdempotencyRecordRepository;
import com.wallettransfer.repo.LedgerEntryRepository;
import com.wallettransfer.repo.TransferRepository;
import com.wallettransfer.repo.WalletRepository;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
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
 * Behavioral integration tests — assertions are made through HTTP API responses and observable
 * wallet balances, not internal repository or service implementation details.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
class TransferIntegrationTest {

  private static final String TRANSFERS_URL = "/v1/transfers";
  private static final String BALANCE_URL = "/v1/wallet-balance";
  private static final String HISTORY_URL = "/v1/transfer-history";

  @Autowired private MockMvc mockMvc;
  @Autowired private ObjectMapper objectMapper;
  @Autowired private WalletRepository walletRepository;
  @Autowired private IdempotencyRecordRepository idempotencyRecordRepository;
  @Autowired private LedgerEntryRepository ledgerEntryRepository;
  @Autowired private TransferRepository transferRepository;

  @BeforeEach
  void seedWallets() {
    idempotencyRecordRepository.deleteAll();
    ledgerEntryRepository.deleteAll();
    transferRepository.deleteAll();
    walletRepository.deleteAll();
    walletRepository.save(Wallet.builder().walletId("wallet_1").balance(1000L).version(0L).build());
    walletRepository.save(Wallet.builder().walletId("wallet_2").balance(500L).version(0L).build());
    walletRepository.save(Wallet.builder().walletId("wallet_3").balance(0L).version(0L).build());
  }

  @Test
  void shouldProcessTransferAndKeepLedgerBalanced() throws Exception {
    performTransfer("key-1", "wallet_1", "wallet_2", 100L)
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("SUCCESS"))
        .andExpect(jsonPath("$.responseCode").value(20001))
        .andExpect(jsonPath("$.data.status").value("PROCESSED"))
        .andExpect(jsonPath("$.data.ledgerEntries.length()").value(2))
        .andExpect(jsonPath("$.data.ledgerEntries[?(@.type == 'DEBIT')].amount").value(100))
        .andExpect(jsonPath("$.data.ledgerEntries[?(@.type == 'CREDIT')].amount").value(100));

    assertThat(fetchBalance("wallet_1")).isEqualTo(900L);
    assertThat(fetchBalance("wallet_2")).isEqualTo(600L);
  }

  @Test
  void shouldReturnOriginalResultForDuplicateIdempotencyKeyWithoutDoubleDebit() throws Exception {
    MvcResult first =
        performTransfer("dup-key", "wallet_1", "wallet_2", 50L)
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.data.status").value("PROCESSED"))
            .andReturn();

    MvcResult second =
        performTransfer("dup-key", "wallet_1", "wallet_2", 50L)
            .andExpect(status().isOk())
            .andReturn();

    assertThat(second.getResponse().getContentAsString())
        .isEqualTo(first.getResponse().getContentAsString());
    assertThat(fetchBalance("wallet_1")).isEqualTo(950L);
    assertThat(fetchBalance("wallet_2")).isEqualTo(550L);
  }

  @Test
  void shouldFailTransferWhenInsufficientFundsWithoutChangingBalances() throws Exception {
    performTransfer("insufficient-key", "wallet_3", "wallet_2", 10L)
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("FAIL"))
        .andExpect(jsonPath("$.responseCode").value(5002))
        .andExpect(jsonPath("$.data.status").value("FAILED"))
        .andExpect(jsonPath("$.data.ledgerEntries").isEmpty());

    assertThat(fetchBalance("wallet_3")).isZero();
    assertThat(fetchBalance("wallet_2")).isEqualTo(500L);
  }

  @Test
  void shouldRejectSameIdempotencyKeyWithDifferentPayload() throws Exception {
    performTransfer("conflict-key", "wallet_1", "wallet_2", 25L).andExpect(status().isOk());

    performTransfer("conflict-key", "wallet_1", "wallet_2", 30L)
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("FAIL"))
        .andExpect(jsonPath("$.responseCode").value(5003));

    assertThat(fetchBalance("wallet_1")).isEqualTo(975L);
  }

  @Test
  void shouldRejectTransferWhenSourceAndDestinationAreSame() throws Exception {
    performTransfer("same-wallet-key", "wallet_1", "wallet_1", 10L)
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("FAIL"))
        .andExpect(jsonPath("$.responseCode").value(6017));

    assertThat(fetchBalance("wallet_1")).isEqualTo(1000L);
  }

  @Test
  void shouldRejectTransferWhenWalletDoesNotExist() throws Exception {
    performTransfer("missing-wallet-key", "wallet_missing", "wallet_2", 10L)
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("FAIL"))
        .andExpect(jsonPath("$.responseCode").value(5113));

    assertThat(fetchBalance("wallet_2")).isEqualTo(500L);
  }

  @Test
  void shouldReturnWalletBalanceViaApi() throws Exception {
    mockMvc
        .perform(
            post(BALANCE_URL)
                .contentType(MediaType.APPLICATION_JSON)
                .content(
                    objectMapper.writeValueAsString(
                        WalletBalanceRequestDTO.builder().walletId("wallet_1").build())))
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("SUCCESS"))
        .andExpect(jsonPath("$.responseCode").value(20002))
        .andExpect(jsonPath("$.data.walletId").value("wallet_1"))
        .andExpect(jsonPath("$.data.balance").value(1000));
  }

  @Test
  void shouldReturnTransferHistoryAfterSuccessfulTransfers() throws Exception {
    performTransfer("history-1", "wallet_1", "wallet_2", 40L).andExpect(status().isOk());
    performTransfer("history-2", "wallet_2", "wallet_1", 15L).andExpect(status().isOk());

    mockMvc
        .perform(
            post(HISTORY_URL)
                .contentType(MediaType.APPLICATION_JSON)
                .content(
                    objectMapper.writeValueAsString(
                        TransferHistoryRequestDTO.builder().walletId("wallet_1").build())))
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.status").value("SUCCESS"))
        .andExpect(jsonPath("$.responseCode").value(20003))
        .andExpect(jsonPath("$.data.length()").value(2));
  }

  @Test
  void shouldReturnOriginalFailedResultForDuplicateIdempotencyKey() throws Exception {
    MvcResult first =
        performTransfer("failed-dup-key", "wallet_3", "wallet_2", 10L)
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("FAIL"))
            .andExpect(jsonPath("$.responseCode").value(5002))
            .andExpect(jsonPath("$.data.status").value("FAILED"))
            .andReturn();

    MvcResult second =
        performTransfer("failed-dup-key", "wallet_3", "wallet_2", 10L)
            .andExpect(status().isOk())
            .andReturn();

    assertThat(second.getResponse().getContentAsString())
        .isEqualTo(first.getResponse().getContentAsString());
    assertThat(fetchBalance("wallet_3")).isZero();
    assertThat(fetchBalance("wallet_2")).isEqualTo(500L);
  }

  @Test
  void shouldReturnSingleProcessedTransferForConcurrentDuplicateIdempotencyKey() throws Exception {
    String idempotencyKey = "concurrent-dup-key";

    try (ExecutorService executor = Executors.newFixedThreadPool(2)) {
      CountDownLatch ready = new CountDownLatch(2);
      CountDownLatch start = new CountDownLatch(1);
      List<String> responses = new ArrayList<>();

      Future<?> first =
          executor.submit(
              () -> {
                ready.countDown();
                awaitStart(start);
                responses.add(transferResponseBody("wallet_1", "wallet_2", 20L, idempotencyKey));
              });
      Future<?> second =
          executor.submit(
              () -> {
                ready.countDown();
                awaitStart(start);
                responses.add(transferResponseBody("wallet_1", "wallet_2", 20L, idempotencyKey));
              });

      ready.await();
      start.countDown();
      first.get();
      second.get();

      executor.shutdown();
      assertThat(executor.awaitTermination(30, TimeUnit.SECONDS)).isTrue();

      assertThat(responses).hasSize(2);
      assertThat(responses.get(0)).isEqualTo(responses.get(1));
      assertThat(responses.get(0)).contains("\"status\":\"PROCESSED\"");
      assertThat(fetchBalance("wallet_1")).isEqualTo(980L);
      assertThat(fetchBalance("wallet_2")).isEqualTo(520L);
    }
  }

  @Test
  void shouldPreventDoubleSpendingUnderConcurrentTransfers() throws Exception {
    walletRepository.save(Wallet.builder().walletId("wallet_4").balance(100L).version(0L).build());

    try (ExecutorService executor = Executors.newFixedThreadPool(2)) {
      CountDownLatch ready = new CountDownLatch(2);
      CountDownLatch start = new CountDownLatch(1);
      List<String> outcomes = new ArrayList<>();

      Future<?> first =
          executor.submit(
              () -> {
                ready.countDown();
                awaitStart(start);
                outcomes.add(transferOutcome("wallet_4", "wallet_2", 100L, "concurrent-1"));
              });
      Future<?> second =
          executor.submit(
              () -> {
                ready.countDown();
                awaitStart(start);
                outcomes.add(transferOutcome("wallet_4", "wallet_2", 100L, "concurrent-2"));
              });

      ready.await();
      start.countDown();
      first.get();
      second.get();

      executor.shutdown();
      assertThat(executor.awaitTermination(30, TimeUnit.SECONDS)).isTrue();

      long processedCount = outcomes.stream().filter("PROCESSED"::equals).count();
      assertThat(processedCount).isOne();
      assertThat(fetchBalance("wallet_4")).isZero();
      assertThat(fetchBalance("wallet_2")).isEqualTo(600L);
    }
  }

  private org.springframework.test.web.servlet.ResultActions performTransfer(
      String idempotencyKey, String fromWalletId, String toWalletId, long amount)
      throws Exception {
    CreateTransferRequestDTO request =
        CreateTransferRequestDTO.builder()
            .idempotencyKey(idempotencyKey)
            .fromWalletId(fromWalletId)
            .toWalletId(toWalletId)
            .amount(amount)
            .build();

    return mockMvc.perform(
        post(TRANSFERS_URL)
            .contentType(MediaType.APPLICATION_JSON)
            .content(objectMapper.writeValueAsString(request)));
  }

  private long fetchBalance(String walletId) throws Exception {
    MvcResult result =
        mockMvc
            .perform(
                post(BALANCE_URL)
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(
                        objectMapper.writeValueAsString(
                            WalletBalanceRequestDTO.builder().walletId(walletId).build())))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("SUCCESS"))
            .andReturn();

    JsonNode data = objectMapper.readTree(result.getResponse().getContentAsString()).path("data");
    return data.path("balance").asLong();
  }

  private String transferResponseBody(
      String fromWalletId, String toWalletId, long amount, String idempotencyKey) {
    try {
      MvcResult result =
          performTransfer(idempotencyKey, fromWalletId, toWalletId, amount).andReturn();
      return result.getResponse().getContentAsString();
    } catch (InterruptedException ex) {
      Thread.currentThread().interrupt();
      return "INTERRUPTED";
    } catch (Exception ex) {
      return "ERROR";
    }
  }

  private String transferOutcome(
      String fromWalletId, String toWalletId, long amount, String idempotencyKey) {
    try {
      MvcResult result =
          performTransfer(idempotencyKey, fromWalletId, toWalletId, amount).andReturn();
      JsonNode data = objectMapper.readTree(result.getResponse().getContentAsString()).path("data");
      return data.path("status").asText();
    } catch (InterruptedException ex) {
      Thread.currentThread().interrupt();
      return "INTERRUPTED";
    } catch (Exception ex) {
      return "ERROR";
    }
  }

  private void awaitStart(CountDownLatch start) {
    try {
      start.await();
    } catch (InterruptedException ex) {
      Thread.currentThread().interrupt();
    }
  }
}
