package com.example.wallet.handler;

import com.example.wallet.domain.LedgerEntry;
import java.util.UUID;

public record LedgerEntryResponse(long entryId, String walletId, UUID transferId, String type, long amount) {

    static LedgerEntryResponse from(LedgerEntry e) {
        return new LedgerEntryResponse(e.id(), e.walletId(), e.transferId(), e.type().name(), e.amount());
    }
}
