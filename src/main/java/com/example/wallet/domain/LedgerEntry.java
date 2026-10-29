package com.example.wallet.domain;

import java.time.Instant;
import java.util.UUID;

public record LedgerEntry(
        long id, String walletId, UUID transferId, EntryType type, long amount, Instant createdAt) {}
