package com.wallet.transferservice.repository;

import com.wallet.transferservice.domain.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;

@Repository
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, UUID> {

    List<LedgerEntry> findByTransferId(UUID transferId);

    List<LedgerEntry> findByWalletIdOrderByCreatedAtDesc(UUID walletId);

    /**
     * Sum of ALL ledger entry amounts across the entire system.
     * Must always equal zero (double-entry invariant).
     */
    @Query("SELECT COALESCE(SUM(e.amount), 0) FROM LedgerEntry e")
    BigDecimal sumAllAmounts();

    /**
     * Sum of ledger entry amounts for a specific wallet.
     * Must always equal the wallet's materialized balance.
     */
    @Query("SELECT COALESCE(SUM(e.amount), 0) FROM LedgerEntry e WHERE e.walletId = :walletId")
    BigDecimal sumAmountsByWalletId(UUID walletId);
}
