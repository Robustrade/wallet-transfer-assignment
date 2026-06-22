package com.walletTransfer.walletTransfer.factory;


import com.walletTransfer.walletTransfer.entity.LedgerEntry;
import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.entity.Wallet;
    import com.walletTransfer.walletTransfer.enums.LedgerEntryType;
import org.springframework.stereotype.Component;

import java.time.LocalDateTime;
import java.util.UUID;

@Component
public class LedgerEntryFactory {
    public LedgerEntry createDebitEntry(
            Wallet wallet,
            Transfer transfer,
            Long amount) {

        return LedgerEntry.builder()
                .id(UUID.randomUUID())
                .wallet(wallet)
                .transfer(transfer)
                .type(LedgerEntryType.DEBIT)
                .amount(amount)
                .createdAt(LocalDateTime.now())
                .build();
    }

    public LedgerEntry createCreditEntry(
            Wallet wallet,
            Transfer transfer,
            Long amount) {

        return LedgerEntry.builder()
                .id(UUID.randomUUID())
                .wallet(wallet)
                .transfer(transfer)
                .type(LedgerEntryType.CREDIT)
                .amount(amount)
                .createdAt(LocalDateTime.now())
                .build();
    }
}
