package com.kulu.wallet.domain;

import java.time.LocalDateTime;

public record LedgerEntry(
    long id,
    String transferId,
    long walletId,
    EntryType type,
    long amount,
    LocalDateTime createdAt
) {
    public enum EntryType { DEBIT, CREDIT }
}
