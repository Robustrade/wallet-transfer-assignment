package com.wallettransfer.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.FailureReason;
import com.wallettransfer.domain.IdempotencyKeyReusedException;
import com.wallettransfer.domain.TransferState;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.LedgerInvariants;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

class TransferServiceIdempotencyTest extends PostgresIntegrationTest {

  @Autowired private TransferService service;
  @Autowired private WalletRepository wallets;
  @Autowired private LedgerRepository ledger;

  @BeforeEach
  void seedWallets() {
    Instant now = Instant.parse("2026-01-02T03:04:05Z");
    wallets.create(Wallet.open("w1", 100, now));
    wallets.create(Wallet.open("w2", 50, now));
  }

  private long balanceOf(String id) {
    return wallets.findById(id).orElseThrow().balance();
  }

  private int transferCount() {
    Integer count = jdbc.queryForObject("SELECT COUNT(*) FROM transfers", Integer.class);
    return count == null ? 0 : count;
  }

  private static CreateTransferCommand command(String key, long amount) {
    return new CreateTransferCommand(key, "w1", "w2", amount);
  }

  @Test
  void sameKeyReturnsTheOriginalResultWithoutMovingMoneyTwice() {
    TransferResult first = service.createTransfer(command("abc123", 30));
    TransferResult second = service.createTransfer(command("abc123", 30));

    assertThat(first.replayed()).isFalse();
    assertThat(second.replayed()).isTrue();
    assertThat(second.transfer().id()).as("the original transfer").isEqualTo(first.transfer().id());
    assertThat(second.transfer().state()).isEqualTo(TransferState.PROCESSED);
    assertThat(balanceOf("w1")).as("debited once").isEqualTo(70);
    assertThat(balanceOf("w2")).as("credited once").isEqualTo(80);
    assertThat(transferCount()).isEqualTo(1);
    assertThat(ledger.findByTransferId(first.transfer().id())).hasSize(2);
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void replayOfAFailedTransferReturnsTheSameFailureEvenIfFundsArriveLater() {
    TransferResult first = service.createTransfer(command("k", 500));
    assertThat(first.transfer().state()).isEqualTo(TransferState.FAILED);

    wallets.credit("w1", 1_000, Instant.parse("2026-01-02T03:05:00Z"));
    TransferResult second = service.createTransfer(command("k", 500));

    assertThat(second.replayed()).isTrue();
    assertThat(second.transfer().id()).isEqualTo(first.transfer().id());
    assertThat(second.transfer().state()).isEqualTo(TransferState.FAILED);
    assertThat(second.transfer().failureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
    assertThat(transferCount()).isEqualTo(1);
  }

  @Test
  void reusingAKeyWithADifferentPayloadIsRejected() {
    service.createTransfer(command("k", 30));

    assertThatThrownBy(() -> service.createTransfer(command("k", 31)))
        .isInstanceOf(IdempotencyKeyReusedException.class);

    assertThat(balanceOf("w1")).as("the conflicting request must not move money").isEqualTo(70);
    assertThat(transferCount()).isEqualTo(1);
  }

  @Test
  void aBlankKeyIsTreatedAsNoKey() {
    TransferResult first = service.createTransfer(command("  ", 10));
    TransferResult second = service.createTransfer(command("  ", 10));

    assertThat(second.replayed()).isFalse();
    assertThat(second.transfer().id()).isNotEqualTo(first.transfer().id());
    assertThat(first.transfer().idempotencyKey()).isNull();
  }

  @Test
  void aRejectedRequestDoesNotBurnItsKey() {
    assertThatThrownBy(
            () -> service.createTransfer(new CreateTransferCommand("k", "w1", "ghost", 10)))
        .isInstanceOf(com.wallettransfer.domain.WalletNotFoundException.class);

    TransferResult retry = service.createTransfer(command("k", 10));

    assertThat(retry.replayed()).isFalse();
    assertThat(retry.transfer().state()).isEqualTo(TransferState.PROCESSED);
  }
}
