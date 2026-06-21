package com.rajat.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.rajat.wallet.domain.enums.TransferStatus;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.support.AbstractIntegrationTest;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * The hard guarantees: no double spending under concurrent debits, and no duplicate side effects
 * when the same idempotency key arrives concurrently. These drive real HTTP requests from a thread
 * pool so each request runs on its own connection/transaction — the only way to exercise the row
 * locks.
 */
class ConcurrencyIT extends AbstractIntegrationTest {

  @Test
  void concurrentDebitsOfTheSameWalletNeverOverdraw() throws Exception {
    // 100.00 balance, ten simultaneous debits of 20.00 each -> exactly five can succeed.
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");
    int attempts = 10;

    List<ResponseEntity<TransferResponse>> responses =
        runConcurrently(
            attempts, i -> () -> postTransfer(transfer("debit-" + i, source, dest, "20.00")));

    long succeeded =
        responses.stream()
            .filter(r -> r.getStatusCode() == HttpStatus.CREATED)
            .filter(r -> r.getBody() != null && r.getBody().status() == TransferStatus.PROCESSED)
            .count();
    long failed =
        responses.stream()
            .filter(r -> r.getStatusCode() == HttpStatus.UNPROCESSABLE_ENTITY)
            .count();

    assertThat(succeeded).isEqualTo(5);
    assertThat(failed).isEqualTo(5);

    // No overdraft: the source is drained to exactly zero, never negative.
    assertThat(balanceOf(source)).isEqualByComparingTo("0.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("100.00");

    // Ledger stays consistent: two entries per successful transfer, none for the failures.
    assertThat(ledgerEntryRepository.count()).isEqualTo(2L * succeeded);
  }

  @Test
  void concurrentDuplicateKeyAppliesTheTransferExactlyOnce() throws Exception {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");
    int attempts = 6;

    List<ResponseEntity<TransferResponse>> responses =
        runConcurrently(
            attempts, i -> () -> postTransfer(transfer("same-key", source, dest, "30.00")));

    // The side effect happened once, regardless of how many duplicates raced.
    assertThat(transferRepository.count()).isEqualTo(1);
    assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    assertThat(balanceOf(source)).isEqualByComparingTo("70.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("30.00");

    // Every concurrent duplicate blocks on the unique index and then replays the winner, so they
    // all return 201 with the same transfer id — no caller observes a committed IN_PROGRESS and
    // gets a fast 409 under this reservation strategy.
    assertThat(responses)
        .allSatisfy(r -> assertThat(r.getStatusCode()).isEqualTo(HttpStatus.CREATED));
    List<UUID> transferIds =
        responses.stream().map(r -> r.getBody().transferId()).distinct().toList();
    assertThat(transferIds).hasSize(1);
  }

  /** Fires {@code count} tasks as simultaneously as possible and returns their results in order. */
  private <T> List<T> runConcurrently(int count, java.util.function.IntFunction<Callable<T>> task)
      throws Exception {
    ExecutorService pool = Executors.newFixedThreadPool(count);
    CountDownLatch ready = new CountDownLatch(count);
    CountDownLatch start = new CountDownLatch(1);
    try {
      List<Future<T>> futures = new ArrayList<>();
      for (int i = 0; i < count; i++) {
        Callable<T> work = task.apply(i);
        futures.add(
            pool.submit(
                () -> {
                  ready.countDown();
                  start.await();
                  return work.call();
                }));
      }
      // Fail loudly if the workers never all reach the gate — otherwise the test could silently
      // stop being concurrent (and stop validating what it claims) while still passing.
      assertThat(ready.await(10, TimeUnit.SECONDS))
          .as("all %d worker threads reached the start gate", count)
          .isTrue();
      start.countDown(); // release them together

      List<T> results = new ArrayList<>();
      for (Future<T> future : futures) {
        results.add(future.get(30, TimeUnit.SECONDS));
      }
      return results;
    } finally {
      pool.shutdownNow();
    }
  }
}
