package com.wallet.domain;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import lombok.Getter;

@Getter
public class LedgerEntry {

    private final UUID id;
    private final UUID transferId;
    private final UUID walletId;
    private final EntryType entryType;
    private final BigDecimal amount;
    private final Instant createdAt;

    public LedgerEntry(UUID id, UUID transferId, UUID walletId, EntryType entryType, BigDecimal amount,
            Instant createdAt) {
        BigDecimal normalizedAmount = Money.normalize(amount);
        if (normalizedAmount.signum() <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        this.id = id;
        this.transferId = transferId;
        this.walletId = walletId;
        this.entryType = entryType;
        this.amount = normalizedAmount;
        this.createdAt = createdAt;
    }
}
