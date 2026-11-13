package com.wallettransfer.domain;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Instant;
import org.junit.jupiter.api.Test;

class LedgerEntryTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  private final Transfer transfer = Transfer.create("t1", null, "wallet_1", "wallet_2", 40, NOW);

  @Test
  void debitHitsSourceWalletForTheTransferAmount() {
    LedgerEntry debit = LedgerEntry.debit(transfer, "e1", NOW);

    assertThat(debit.id()).isEqualTo("e1");
    assertThat(debit.transferId()).isEqualTo("t1");
    assertThat(debit.walletId()).isEqualTo("wallet_1");
    assertThat(debit.type()).isEqualTo(EntryType.DEBIT);
    assertThat(debit.amount()).isEqualTo(40);
    assertThat(debit.createdAt()).isEqualTo(NOW);
  }

  @Test
  void creditHitsDestinationWalletForTheSameAmount() {
    LedgerEntry credit = LedgerEntry.credit(transfer, "e2", NOW);

    assertThat(credit.walletId()).isEqualTo("wallet_2");
    assertThat(credit.type()).isEqualTo(EntryType.CREDIT);
    assertThat(credit.amount()).isEqualTo(transfer.amount());
    assertThat(credit.transferId()).isEqualTo("t1");
  }
}
