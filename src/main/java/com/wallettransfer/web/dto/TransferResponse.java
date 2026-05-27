package com.wallettransfer.web.dto;

import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferStatus;
import java.time.Instant;
import java.util.List;

/**
 * API response for a transfer including status and ledger rows.
 */
public record TransferResponse(
        String id,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferStatus status,
        String failureReason,
        Instant createdAt,
        Instant updatedAt,
        List<LedgerEntryResponse> ledgerEntries) {

    /**
     * Maps domain transfer and ledger entities to the HTTP response shape.
     *
     * @param transfer       persisted transfer
     * @param ledgerEntries  debit/credit rows (empty if failed)
     * @return response DTO
     */
    public static TransferResponse from(Transfer transfer, List<LedgerEntry> ledgerEntries) {
        List<LedgerEntryResponse> ledger = ledgerEntries.stream().map(LedgerEntryResponse::from).toList();
        return new TransferResponse(
                transfer.getId(),
                transfer.getIdempotencyKey(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getFailureReason(),
                transfer.getCreatedAt(),
                transfer.getUpdatedAt(),
                ledger);
    }
}
