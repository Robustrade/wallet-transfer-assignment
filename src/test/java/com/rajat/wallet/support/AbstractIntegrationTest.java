package com.rajat.wallet.support;

import com.rajat.wallet.domain.entities.Wallet;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.repository.IdempotencyRecordRepository;
import com.rajat.wallet.repository.LedgerEntryRepository;
import com.rajat.wallet.repository.TransferRepository;
import com.rajat.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;

/**
 * Base for behavioural integration tests. Boots the full Spring context against a real PostgreSQL
 * (via Testcontainers) so Flyway migrations, pessimistic row locks and JPA mapping are all
 * exercised exactly as in production — the things that actually guarantee correctness can only be
 * tested on the real engine, not an in-memory substitute.
 *
 * <p>The container is started once for the whole JVM (the static singleton pattern) and reused
 * across every test class; each test starts from a clean set of tables via {@link
 * #resetDatabase()}.
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
public abstract class AbstractIntegrationTest {

  static final PostgreSQLContainer<?> POSTGRES =
      new PostgreSQLContainer<>("postgres:15.4")
          .withDatabaseName("wallet")
          .withUsername("wallet")
          .withPassword("wallet");

  static {
    POSTGRES.start();
  }

  @DynamicPropertySource
  static void datasourceProperties(DynamicPropertyRegistry registry) {
    registry.add("spring.datasource.url", POSTGRES::getJdbcUrl);
    registry.add("spring.datasource.username", POSTGRES::getUsername);
    registry.add("spring.datasource.password", POSTGRES::getPassword);
  }

  @Autowired protected TestRestTemplate restTemplate;
  @Autowired protected WalletRepository walletRepository;
  @Autowired protected TransferRepository transferRepository;
  @Autowired protected LedgerEntryRepository ledgerEntryRepository;
  @Autowired protected IdempotencyRecordRepository idempotencyRepository;

  @BeforeEach
  void resetDatabase() {
    // FK-safe order: ledger and transfers reference wallets; idempotency is independent.
    ledgerEntryRepository.deleteAllInBatch();
    transferRepository.deleteAllInBatch();
    idempotencyRepository.deleteAllInBatch();
    walletRepository.deleteAllInBatch();
  }

  protected UUID seedWallet(String balance, String currency) {
    Wallet wallet = walletRepository.saveAndFlush(new Wallet(new BigDecimal(balance), currency));
    return wallet.getId();
  }

  protected CreateTransferRequest transfer(
      String idempotencyKey, UUID from, UUID to, String amount) {
    return new CreateTransferRequest(idempotencyKey, from, to, new BigDecimal(amount));
  }

  /** Posts a transfer expecting a transfer-shaped body (used for 201 PROCESSED and 422 FAILED). */
  protected ResponseEntity<TransferResponse> postTransfer(CreateTransferRequest request) {
    return restTemplate.postForEntity("/transfers", request, TransferResponse.class);
  }

  /** Posts and reads the body as a raw String — safe for error responses (400/404/409). */
  protected ResponseEntity<String> postForString(Object body) {
    return restTemplate.postForEntity("/transfers", body, String.class);
  }

  /** Posts a raw JSON string with an explicit content type, for malformed-body cases. */
  protected ResponseEntity<String> postJson(String json) {
    HttpHeaders headers = new HttpHeaders();
    headers.setContentType(MediaType.APPLICATION_JSON);
    return restTemplate.postForEntity("/transfers", new HttpEntity<>(json, headers), String.class);
  }

  protected BigDecimal balanceOf(UUID walletId) {
    return walletRepository.findById(walletId).orElseThrow().getBalance();
  }
}
