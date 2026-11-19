package com.wallettransfer.repository;

import com.wallettransfer.domain.LedgerEntry;
import java.util.List;

/** Persistence port for the append-only double-entry ledger. */
public interface LedgerRepository {

  /** Appends entries. Callers write both sides of a transfer in the same transaction. */
  void insert(LedgerEntry... entries);

  /** The entries of a transfer, DEBIT first. */
  List<LedgerEntry> findByTransferId(String transferId);
}
