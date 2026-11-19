package com.wallettransfer.domain;

import java.time.Instant;

/** One immutable side of a processed transfer. Every transfer yields exactly one of each side. */
public record LedgerEntry(
    String id, String transferId, String walletId, EntryType type, long amount, Instant createdAt) {

  /** The DEBIT on the transfer's source wallet. */
  public static LedgerEntry debit(Transfer transfer, String id, Instant now) {
    return new LedgerEntry(
        id, transfer.id(), transfer.fromWalletId(), EntryType.DEBIT, transfer.amount(), now);
  }

  /** The CREDIT on the transfer's destination wallet. */
  public static LedgerEntry credit(Transfer transfer, String id, Instant now) {
    return new LedgerEntry(
        id, transfer.id(), transfer.toWalletId(), EntryType.CREDIT, transfer.amount(), now);
  }
}
