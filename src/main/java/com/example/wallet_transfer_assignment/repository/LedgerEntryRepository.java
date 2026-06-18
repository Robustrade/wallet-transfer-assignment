package com.example.wallet_transfer_assignment.repository;


import com.example.wallet_transfer_assignment.entity.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.List;
import java.util.UUID;

public interface LedgerEntryRepository
        extends JpaRepository<LedgerEntry, UUID> {

    @Query("""
       SELECT le
       FROM LedgerEntry le
       JOIN FETCH le.transfer t
       WHERE le.wallet.id = :walletId
       ORDER BY le.createdAt DESC
       """)
    List<LedgerEntry> findTransactionHistory(
            @Param("walletId") UUID walletId);
}