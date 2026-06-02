package com.walletservice.infrastructure.repository;

import com.walletservice.domain.entity.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.UUID;

/**
 * Repository for LedgerEntry persistence operations.
 */
@Repository
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, String> {
    /**
     * Find all ledger entries for a specific transfer.
     * Used to verify that exactly two entries exist (debit and credit).
     */
    List<LedgerEntry> findByTransferId(@Param("transferId") String transferId);

    /**
     * Find all ledger entries for a specific wallet.
     * Used for balance verification and audit trails.
     */
    List<LedgerEntry> findByWalletId(@Param("walletId") String walletId);
}

