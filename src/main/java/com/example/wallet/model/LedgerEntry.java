package com.example.wallet.model;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

public record LedgerEntry(
        UUID id,
        UUID transferId,
        String walletId,
        EntryType type,
        BigDecimal amount,
        Instant createdAt
) {}

