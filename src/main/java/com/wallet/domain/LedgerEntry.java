package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

public record LedgerEntry(
        long id,
        String walletId,
        UUID transferId,
        LedgerEntryType entryType,
        long amount,
        Instant createdAt) {}
