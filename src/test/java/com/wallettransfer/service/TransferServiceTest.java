package com.wallettransfer.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.EntryType;
import com.wallettransfer.domain.FailureReason;
import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferNotFoundException;
import com.wallettransfer.domain.TransferState;
import com.wallettransfer.domain.ValidationException;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletNotFoundException;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.LedgerInvariants;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import java.util.List;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.springframework.beans.factory.annotation.Autowired;

class TransferServiceTest extends PostgresIntegrationTest {

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
  void movesMoneyAndWritesABalancedLedger() {
    TransferResult result = service.createTransfer(command(null, 30));

    assertThat(result.replayed()).isFalse();
    assertThat(result.transfer().state()).isEqualTo(TransferState.PROCESSED);
    assertThat(balanceOf("w1")).isEqualTo(70);
    assertThat(balanceOf("w2")).isEqualTo(80);

    List<LedgerEntry> entries = ledger.findByTransferId(result.transfer().id());
    assertThat(entries).hasSize(2);
    assertThat(entries.get(0).type()).isEqualTo(EntryType.DEBIT);
    assertThat(entries.get(0).walletId()).isEqualTo("w1");
    assertThat(entries.get(1).type()).isEqualTo(EntryType.CREDIT);
    assertThat(entries.get(1).walletId()).isEqualTo("w2");
    LedgerInvariants.assertHold(jdbc);
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource({
    "zero amount,w1,w2,0",
    "negative amount,w1,w2,-1",
    "same wallet,w1,w1,5",
    "missing wallet id,,w2,5"
  })
  void rejectsInvalidRequestsAndPersistsNothing(String name, String from, String to, long amount) {
    assertThatThrownBy(
            () -> service.createTransfer(new CreateTransferCommand(null, from, to, amount)))
        .isInstanceOf(ValidationException.class);

    assertThat(transferCount()).isZero();
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource({"unknown source,ghost,w2", "unknown destination,w1,ghost"})
  void unknownWalletPersistsNothing(String name, String from, String to) {
    assertThatThrownBy(() -> service.createTransfer(new CreateTransferCommand(null, from, to, 5)))
        .isInstanceOf(WalletNotFoundException.class);

    assertThat(transferCount()).isZero();
    assertThat(balanceOf("w1")).isEqualTo(100);
    assertThat(balanceOf("w2")).isEqualTo(50);
  }

  @Test
  void insufficientFundsIsRecordedAsAFailedTransfer() {
    TransferResult result = service.createTransfer(command(null, 101));

    assertThat(result.transfer().state()).isEqualTo(TransferState.FAILED);
    assertThat(result.transfer().failureReason()).isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
    assertThat(service.getTransfer(result.transfer().id()).state())
        .as("the failed attempt must be persisted")
        .isEqualTo(TransferState.FAILED);
    assertThat(balanceOf("w1")).as("no money may move").isEqualTo(100);
    assertThat(balanceOf("w2")).isEqualTo(50);
    assertThat(ledger.findByTransferId(result.transfer().id())).isEmpty();
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void canSpendTheExactBalance() {
    TransferResult result = service.createTransfer(command(null, 100));

    assertThat(result.transfer().state()).isEqualTo(TransferState.PROCESSED);
    assertThat(balanceOf("w1")).isZero();
  }

  @Test
  void withoutAKeyEveryCallIsANewTransfer() {
    Transfer first = service.createTransfer(command(null, 10)).transfer();
    Transfer second = service.createTransfer(command(null, 10)).transfer();

    assertThat(first.id()).isNotEqualTo(second.id());
    assertThat(balanceOf("w1")).isEqualTo(80);
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void unknownTransferIsNotFound() {
    assertThatThrownBy(() -> service.getTransfer("nope"))
        .isInstanceOf(TransferNotFoundException.class);
  }
}
