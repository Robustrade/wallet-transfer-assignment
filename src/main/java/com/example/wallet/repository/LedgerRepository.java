package com.example.wallet.repository;

import com.example.wallet.model.LedgerEntry;

import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;

public interface LedgerRepository {

    /**
     * Insert a DEBIT entry for the given transfer and wallet.
     */
    void insertDebit(UUID transferId, String walletId, BigDecimal amount);

    /**
     * Insert a CREDIT entry for the given transfer and wallet.
     */
    void insertCredit(UUID transferId, String walletId, BigDecimal amount);

    /**
     * Retrieve all ledger entries for a given transfer.
     * Should always return exactly 2 entries (one DEBIT, one CREDIT).
     */
    List<LedgerEntry> findByTransferId(UUID transferId);

    /**
     * Count ledger entries for a given transfer.
     * Used in tests to verify exactly-once ledger writes.
     */
    int countByTransferId(UUID transferId);
}

