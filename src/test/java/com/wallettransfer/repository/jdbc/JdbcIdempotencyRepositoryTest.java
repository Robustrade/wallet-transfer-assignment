package com.wallettransfer.repository.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;

import com.wallettransfer.domain.IdempotencyRecord;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.IdempotencyRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.transaction.support.TransactionTemplate;

class JdbcIdempotencyRepositoryTest extends PostgresIntegrationTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Autowired private IdempotencyRepository idempotency;
  @Autowired private TransferRepository transfers;
  @Autowired private WalletRepository wallets;
  @Autowired private TransactionTemplate tx;

  @BeforeEach
  void seed() {
    wallets.create(Wallet.open("w1", 100, NOW));
    wallets.create(Wallet.open("w2", 0, NOW));
    transfers.insert(Transfer.create("t1", "k1", "w1", "w2", 10, NOW));
    transfers.insert(Transfer.create("t2", "k1", "w1", "w2", 10, NOW));
  }

  private static IdempotencyRecord record(String key, String transferId) {
    return new IdempotencyRecord(key, "hash-" + transferId, transferId, NOW);
  }

  @Test
  void firstClaimWinsAndCanBeFound() {
    assertThat(idempotency.tryClaim(record("k1", "t1"))).isTrue();

    IdempotencyRecord found = idempotency.findByKey("k1").orElseThrow();

    assertThat(found.key()).isEqualTo("k1");
    assertThat(found.requestHash()).isEqualTo("hash-t1");
    assertThat(found.transferId()).isEqualTo("t1");
    assertThat(found.createdAt()).isEqualTo(NOW);
  }

  @Test
  void secondClaimOfTheSameKeyLosesAndLeavesTheOriginalUntouched() {
    idempotency.tryClaim(record("k1", "t1"));

    assertThat(idempotency.tryClaim(record("k1", "t2"))).isFalse();

    assertThat(idempotency.findByKey("k1").orElseThrow().transferId()).isEqualTo("t1");
  }

  @Test
  void unknownKeyIsEmpty() {
    assertThat(idempotency.findByKey("nope")).isEmpty();
  }

  @Test
  void aRacingClaimWaitsForTheFirstTransactionAndThenLoses() throws Exception {
    CountDownLatch firstHasClaimed = new CountDownLatch(1);
    CountDownLatch commitFirst = new CountDownLatch(1);
    try (ExecutorService executor = Executors.newFixedThreadPool(2)) {
      try {
        Future<Boolean> first =
            executor.submit(
                () ->
                    tx.execute(
                        status -> {
                          boolean won = idempotency.tryClaim(record("k1", "t1"));
                          firstHasClaimed.countDown();
                          awaitQuietly(commitFirst);
                          return won;
                        }));
        firstHasClaimed.await();

        Future<Boolean> second =
            executor.submit(() -> tx.execute(status -> idempotency.tryClaim(record("k1", "t2"))));

        await()
            .during(Duration.ofMillis(300))
            .atMost(Duration.ofSeconds(3))
            .until(() -> !second.isDone());
        commitFirst.countDown();

        assertThat(first.get()).isTrue();
        assertThat(second.get()).as("the loser is told the key is taken").isFalse();
      } finally {
        commitFirst.countDown(); // never leave a transaction blocked if an assertion failed
      }
    }
  }

  private static void awaitQuietly(CountDownLatch latch) {
    try {
      latch.await();
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
