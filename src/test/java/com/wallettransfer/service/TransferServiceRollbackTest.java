package com.wallettransfer.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doThrow;

import com.wallettransfer.domain.LedgerEntry;
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
import org.springframework.test.context.bean.override.mockito.MockitoSpyBean;

class TransferServiceRollbackTest extends PostgresIntegrationTest {

  @Autowired private TransferService service;
  @Autowired private WalletRepository wallets;
  @MockitoSpyBean private LedgerRepository ledger;

  @BeforeEach
  void seedWallets() {
    Instant now = Instant.parse("2026-01-02T03:04:05Z");
    wallets.create(Wallet.open("w1", 100, now));
    wallets.create(Wallet.open("w2", 50, now));
  }

  private long balanceOf(String id) {
    return wallets.findById(id).orElseThrow().balance();
  }

  private int countOf(String table) {
    Integer count = jdbc.queryForObject("SELECT COUNT(*) FROM " + table, Integer.class);
    return count == null ? 0 : count;
  }

  @Test
  void aFailureAfterTheDebitRollsBackTheWholeTransferAndTheRetrySucceeds() {
    doThrow(new RuntimeException("ledger unavailable"))
        .doCallRealMethod()
        .when(ledger)
        .insert(any(LedgerEntry.class), any(LedgerEntry.class));

    CreateTransferCommand command = new CreateTransferCommand("retry-key", "w1", "w2", 30);

    assertThatThrownBy(() -> service.createTransfer(command))
        .isInstanceOf(RuntimeException.class)
        .hasMessage("ledger unavailable");

    assertThat(balanceOf("w1")).as("the debit must be rolled back").isEqualTo(100);
    assertThat(balanceOf("w2")).as("the credit must be rolled back").isEqualTo(50);
    assertThat(countOf("transfers")).as("no transfer may be left behind").isZero();
    assertThat(countOf("ledger_entries")).as("no ledger entry may be left behind").isZero();
    assertThat(countOf("idempotency_records"))
        .as("the idempotency claim must be rolled back")
        .isZero();

    TransferResult retry = service.createTransfer(command);

    assertThat(retry.replayed()).as("the retry is a fresh execution, not a replay").isFalse();
    assertThat(retry.transfer().state()).isEqualTo(TransferState.PROCESSED);
    assertThat(balanceOf("w1")).isEqualTo(70);
    assertThat(balanceOf("w2")).isEqualTo(80);
    assertThat(countOf("transfers")).isEqualTo(1);
    LedgerInvariants.assertHold(jdbc);
  }
}
