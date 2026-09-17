package com.robustrade.wallet;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.argThat;
import static org.mockito.Mockito.doThrow;

import com.robustrade.wallet.dto.CreateTransferRequest;
import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.LedgerEntryType;
import com.robustrade.wallet.enums.TransferStatus;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.service.TransferResult;
import com.robustrade.wallet.service.TransferService;
import com.robustrade.wallet.support.IntegrationTest;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean;

/**
 * The properties that only a real database can demonstrate: row locks actually serialise competing
 * transactions, a mid-transaction failure leaves nothing behind, and ON CONFLICT collapses a race
 * on one idempotency key into a single execution.
 */
@Tag("integration")
class TransferServiceIntegrationTest extends IntegrationTest {

  private static final UUID CUSTOMER = UUID.fromString("11111111-1111-1111-1111-111111111111");

  @Autowired private TransferService transferService;

  @MockitoSpyBean private LedgerEntryRepository ledgerEntryRepository;

  @Test
  void aTransferMovesMoneyAndWritesBalancedLedgerRows() {
    TransferResult result = transfer("live-1", "wallet_1", "wallet_2", "125");

    assertThat(result.replayed()).isFalse();
    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
    assertThat(balanceOf("wallet_1")).isEqualByComparingTo("875");
    assertThat(balanceOf("wallet_2")).isEqualByComparingTo("625");
    assertThat(ledgerEntryCount()).isEqualTo(2);
    assertThat(ledgerNet()).isEqualByComparingTo("0");
  }

  /** A FAILED transfer must commit, so a retry with the same key gets the same stored answer. */
  @Test
  void insufficientFundsCommitTheFailedTransfer() {
    TransferResult result = transfer("live-poor", "wallet_3", "wallet_1", "1");

    assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.FAILED);
    assertThat(result.transfer().getFailureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
    assertThat(transferCount()).isEqualTo(1);
    assertThat(ledgerEntryCount()).isZero();
    assertThat(balanceOf("wallet_1")).isEqualByComparingTo("1000");
  }

  /**
   * The failure the manual experiment hit: if anything throws after the balances have been touched,
   * the whole transaction must unwind, leaving no transfer row and no half-written ledger.
   */
  @Test
  void aFailureMidTransactionRollsBackEverything() {
    // Only the CREDIT write fails, so the DEBIT row and both balance changes are already
    // pending in the transaction when it blows up. Matching on the argument rather than call
    // order keeps the real repository in play for the first save.
    doThrow(new IllegalStateException("ledger unavailable"))
        .when(ledgerEntryRepository)
        .save(argThat(entry -> entry != null && entry.getType() == LedgerEntryType.CREDIT));

    assertThatThrownBy(() -> transfer("live-rollback", "wallet_1", "wallet_2", "125"))
        .isInstanceOf(IllegalStateException.class);

    assertThat(transferCount()).isZero();
    assertThat(ledgerEntryCount()).isZero();
    assertThat(balanceOf("wallet_1")).isEqualByComparingTo("1000");
    assertThat(balanceOf("wallet_2")).isEqualByComparingTo("500");
  }

  /**
   * Eight transactions competing for the same two rows. Without the FOR UPDATE locks some would
   * read a stale balance and overwrite each other, so a final balance above 920 is a lost update.
   */
  @Test
  void concurrentTransfersOnTheSameWalletsDoNotLoseUpdates() throws Exception {
    List<TransferResult> results = inParallel(8, i -> "concurrent-" + i, "wallet_1", "wallet_2");

    assertThat(results).allSatisfy(r -> assertThat(r.replayed()).isFalse());
    assertThat(balanceOf("wallet_1")).isEqualByComparingTo("920");
    assertThat(balanceOf("wallet_2")).isEqualByComparingTo("580");
    assertThat(transferCount()).isEqualTo(8);
    assertThat(ledgerEntryCount()).isEqualTo(16);
    assertThat(ledgerNet()).isEqualByComparingTo("0");
  }

  /** Eight retries of one request. Exactly one may execute; the rest must replay it. */
  @Test
  void concurrentRequestsSharingAnIdempotencyKeyExecuteOnce() throws Exception {
    List<TransferResult> results = inParallel(8, i -> "same-key", "wallet_1", "wallet_2");

    assertThat(results).filteredOn(r -> !r.replayed()).hasSize(1);
    assertThat(results).filteredOn(TransferResult::replayed).hasSize(7);
    assertThat(balanceOf("wallet_1")).isEqualByComparingTo("990");
    assertThat(balanceOf("wallet_2")).isEqualByComparingTo("510");
    assertThat(transferCount()).isEqualTo(1);
    assertThat(ledgerEntryCount()).isEqualTo(2);
  }

  /** Fires every request from its own thread at once, so they genuinely contend for the locks. */
  private List<TransferResult> inParallel(
      int threads, java.util.function.IntFunction<String> key, String fromId, String toId)
      throws Exception {
    ExecutorService pool = Executors.newFixedThreadPool(threads);
    CountDownLatch startLine = new CountDownLatch(1);
    List<Future<TransferResult>> futures = new ArrayList<>();
    try {
      for (int i = 0; i < threads; i++) {
        String idempotencyKey = key.apply(i);
        futures.add(
            pool.submit(
                () -> {
                  startLine.await();
                  return transfer(idempotencyKey, fromId, toId, "10");
                }));
      }
      startLine.countDown();

      List<TransferResult> results = new ArrayList<>();
      for (Future<TransferResult> future : futures) {
        results.add(future.get(30, TimeUnit.SECONDS));
      }
      return results;
    } finally {
      pool.shutdownNow();
    }
  }

  private TransferResult transfer(String key, String fromId, String toId, String amount) {
    return transferService.createTransfer(
        new CreateTransferRequest(key, fromId, toId, new BigDecimal(amount)), CUSTOMER);
  }
}
