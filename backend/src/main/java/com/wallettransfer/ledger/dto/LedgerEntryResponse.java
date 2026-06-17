package com.wallettransfer.ledger.dto;

import com.wallettransfer.ledger.domain.LedgerEntryType;
import java.time.Instant;
import java.util.UUID;

public record LedgerEntryResponse(
        Long id,
        UUID transferId,
        String walletId,
        LedgerEntryType entryType,
        long amount,
        Instant createdAt
) {
}
