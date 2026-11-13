package com.wallettransfer.service;

import static org.assertj.core.api.Assertions.assertThat;

import com.wallettransfer.domain.TransferState;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.LedgerInvariants;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.IntFunction;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

/** Races many real transactions against PostgreSQL and then checks the accounting invariants. */
class TransferServiceConcurrencyTest extends PostgresIntegrationTest {

  private static final long TIMEOUT_SECONDS = 60;

  @Autowired private TransferService service;
  @Autowired private WalletRepository wallets;

  @BeforeEach
  void seedWallets() {
    Instant now = Instant.parse("2026-01-02T03:04:05Z");
    wallets.create(Wallet.open("w1", 100, now));
    wallets.create(Wallet.open("w2", 50, now));
  }

  private long balanceOf(String id) {
    return wallets.findById(id).orElseThrow().balance();
  }

  /** Starts {@code n} tasks at the same moment and returns their results. */
  private static List<TransferResult> raceAll(int n, IntFunction<TransferResult> task)
      throws InterruptedException, ExecutionException, TimeoutException {
    CountDownLatch ready = new CountDownLatch(n);
    CountDownLatch start = new CountDownLatch(1);
    try (ExecutorService executor = Executors.newFixedThreadPool(n)) {
      List<Future<TransferResult>> futures = new ArrayList<>();
      for (int i = 0; i < n; i++) {
        int index = i;
        futures.add(
            executor.submit(
                () -> {
                  ready.countDown();
                  start.await();
                  return task.apply(index);
                }));
      }
      ready.await();
      start.countDown();
      List<TransferResult> results = new ArrayList<>();
      for (Future<TransferResult> future : futures) {
        results.add(future.get(TIMEOUT_SECONDS, TimeUnit.SECONDS));
      }
      return results;
    }
  }

  @Test
  void parallelDebitsOfOneWalletNeverOverspend() throws Exception {
    int requests = 40;
    long amount = 10; // w1 holds 100, so exactly 10 transfers can succeed

    List<TransferResult> results =
        raceAll(
            requests,
            i -> service.createTransfer(new CreateTransferCommand("key-" + i, "w1", "w2", amount)));

    long processed =
        results.stream().filter(r -> r.transfer().state() == TransferState.PROCESSED).count();
    long failed =
        results.stream().filter(r -> r.transfer().state() == TransferState.FAILED).count();
    assertThat(processed).as("exactly balance / amount transfers succeed").isEqualTo(10);
    assertThat(failed).isEqualTo(requests - 10);
    assertThat(balanceOf("w1")).isZero();
    assertThat(balanceOf("w2")).isEqualTo(150);
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void theSameIdempotencyKeyExecutesExactlyOnce() throws Exception {
    List<TransferResult> results =
        raceAll(
            25, i -> service.createTransfer(new CreateTransferCommand("same-key", "w1", "w2", 30)));

    assertThat(results.stream().filter(r -> !r.replayed()).count())
        .as("only one request may execute")
        .isEqualTo(1);
    assertThat(results.stream().map(r -> r.transfer().id()).distinct().count())
        .as("every caller sees the same transfer")
        .isEqualTo(1);
    assertThat(results).allMatch(r -> r.transfer().state() == TransferState.PROCESSED);
    assertThat(jdbc.queryForObject("SELECT COUNT(*) FROM transfers", Integer.class)).isEqualTo(1);
    assertThat(balanceOf("w1")).isEqualTo(70);
    assertThat(balanceOf("w2")).isEqualTo(80);
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void opposingTransfersDoNotDeadlock() throws Exception {
    List<TransferResult> results =
        raceAll(
            40,
            i ->
                i % 2 == 0
                    ? service.createTransfer(new CreateTransferCommand(null, "w1", "w2", 5))
                    : service.createTransfer(new CreateTransferCommand(null, "w2", "w1", 5)));

    assertThat(results).allMatch(r -> r.transfer().state() == TransferState.PROCESSED);
    assertThat(balanceOf("w1")).as("equal flows in both directions cancel out").isEqualTo(100);
    assertThat(balanceOf("w2")).isEqualTo(50);
    LedgerInvariants.assertHold(jdbc);
  }
}
