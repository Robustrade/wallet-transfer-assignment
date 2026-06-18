package com.wallet.transfer.repository;

import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;

/**
 * Ledger Entry Repository
 * 
 * Data access layer for LedgerEntry entities.
 * 
 * Key Methods:
 * - findByTransferId: Find all entries for a transfer
 * - countByTransferId: Count entries (should always be 2 per transfer)
 * - sumDebitsByWalletId: Calculate total debits for a wallet
 * - sumCreditsByWalletId: Calculate total credits for a wallet
 */
@Repository
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, UUID> {
    
    /**
     * Find all ledger entries for a transfer
     * @param transferId the transfer ID
     * @return list of ledger entries
     */
    List<LedgerEntry> findByTransferId(String transferId);
    
    /**
     * Count ledger entries for a transfer
     * 
     * Invariant: Should always be exactly 2 entries per transfer
     * (1 DEBIT from source + 1 CREDIT to destination)
     * 
     * @param transferId the transfer ID
     * @return count of entries
     */
    long countByTransferId(String transferId);
    
    /**
     * Sum of all DEBIT entries for a wallet
     * 
     * Used for balance reconciliation from ledger.
     * 
     * @param walletId the wallet ID
     * @return total debited amount (null if no entries)
     */
    @Query("SELECT SUM(le.amount) FROM LedgerEntry le WHERE le.walletId = :walletId AND le.entryType = 'DEBIT'")
    BigDecimal sumDebitsByWalletId(@Param("walletId") String walletId);
    
    /**
     * Sum of all CREDIT entries for a wallet
     * 
     * Used for balance reconciliation from ledger.
     * 
     * @param walletId the wallet ID
     * @return total credited amount (null if no entries)
     */
    @Query("SELECT SUM(le.amount) FROM LedgerEntry le WHERE le.walletId = :walletId AND le.entryType = 'CREDIT'")
    BigDecimal sumCreditsByWalletId(@Param("walletId") String walletId);
    
    /**
     * Calculate derived balance from ledger for a wallet
     * 
     * Formula: balance = SUM(CREDIT) - SUM(DEBIT)
     * 
     * @param walletId the wallet ID
     * @return derived balance (null if no entries)
     */
    @Query("SELECT COALESCE(SUM(CASE WHEN le.entryType = 'CREDIT' THEN le.amount ELSE le.amount * -1 END), 0) " +
           "FROM LedgerEntry le WHERE le.walletId = :walletId")
    BigDecimal calculateBalanceForWallet(@Param("walletId") String walletId);
}
