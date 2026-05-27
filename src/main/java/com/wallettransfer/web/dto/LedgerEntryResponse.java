package com.wallettransfer.web.dto;

import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.LedgerEntryType;
import java.time.Instant;

/**
 * API view of a single ledger row (DEBIT or CREDIT).
 */
public record LedgerEntryResponse(
        Long id, String walletId, String transferId, LedgerEntryType type, long amount, Instant createdAt) {

    /**
     * @param entry persisted ledger entry
     * @return API response DTO
     */
    public static LedgerEntryResponse from(LedgerEntry entry) {
        return new LedgerEntryResponse(
                entry.getId(),
                entry.getWalletId(),
                entry.getTransferId(),
                entry.getType(),
                entry.getAmount(),
                entry.getCreatedAt());
    }
}
