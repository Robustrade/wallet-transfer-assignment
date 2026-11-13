package com.wallettransfer.repository.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.EntryType;
import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import java.util.List;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.dao.DataAccessException;

class JdbcLedgerRepositoryTest extends PostgresIntegrationTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Autowired private LedgerRepository ledger;
  @Autowired private TransferRepository transfers;
  @Autowired private WalletRepository wallets;

  private final Transfer transfer = Transfer.create("t1", null, "w1", "w2", 40, NOW);

  @BeforeEach
  void seed() {
    wallets.create(Wallet.open("w1", 100, NOW));
    wallets.create(Wallet.open("w2", 0, NOW));
    transfers.insert(transfer);
  }

  @Test
  void storesBothSidesAndListsDebitFirst() {
    ledger.insert(LedgerEntry.credit(transfer, "e2", NOW), LedgerEntry.debit(transfer, "e1", NOW));

    List<LedgerEntry> entries = ledger.findByTransferId("t1");

    assertThat(entries)
        .extracting(LedgerEntry::type)
        .containsExactly(EntryType.DEBIT, EntryType.CREDIT);
    assertThat(entries).extracting(LedgerEntry::walletId).containsExactly("w1", "w2");
    assertThat(entries).extracting(LedgerEntry::amount).containsExactly(40L, 40L);
    assertThat(entries).extracting(LedgerEntry::createdAt).containsOnly(NOW);
  }

  @Test
  void aTransferWithoutEntriesHasNone() {
    assertThat(ledger.findByTransferId("t1")).isEmpty();
  }

  @Test
  void rejectsEntriesForAnUnknownTransfer() {
    Transfer ghost = Transfer.create("ghost", null, "w1", "w2", 5, NOW);

    assertThatThrownBy(() -> ledger.insert(LedgerEntry.debit(ghost, "e1", NOW)))
        .isInstanceOf(DataAccessException.class);
  }

  @Test
  void rejectsASecondDebitForTheSameTransfer() {
    ledger.insert(LedgerEntry.debit(transfer, "e1", NOW));

    assertThatThrownBy(() -> ledger.insert(LedgerEntry.debit(transfer, "e2", NOW)))
        .isInstanceOf(DataAccessException.class);
  }
}
