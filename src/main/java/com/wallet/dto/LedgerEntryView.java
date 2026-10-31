package com.wallet.dto;

import com.wallet.domain.LedgerEntry;

import java.math.BigDecimal;
import java.util.UUID;

public record LedgerEntryView(UUID entryId, UUID transferId, String type, BigDecimal amount) {
    public static LedgerEntryView from(LedgerEntry entry) {
        return new LedgerEntryView(entry.getId(), entry.getTransferId(), entry.getEntryType().name(), entry.getAmount());
    }
}