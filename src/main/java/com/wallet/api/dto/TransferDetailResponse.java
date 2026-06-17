package com.wallet.api.dto;

import com.wallet.domain.LedgerEntryType;
import com.wallet.domain.TransferStatus;
import java.util.List;

public record TransferDetailResponse(
        String transferId,
        TransferStatus status,
        String fromWalletId,
        String toWalletId,
        long amount,
        String failureReason,
        List<LedgerEntryResponse> ledgerEntries) {

    public record LedgerEntryResponse(
            long entryId, String walletId, LedgerEntryType type, long amount) {}
}
