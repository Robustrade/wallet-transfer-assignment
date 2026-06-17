package com.wallet.transfer.repository;

import com.wallet.transfer.model.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.List;

/**
 * Repository interface for managing {@link LedgerEntry} entities.
 * Handles double-entry ledger database operations for tracking DEBIT and CREDIT logs.
 */
@Repository
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, String> {

    /**
     * Retrieves all ledger entries associated with a specific transfer.
     * This is useful for auditing and verifying double-entry bookkeeping details (DEBIT/CREDIT pairs).
     *
     * @param transferId The ID of the transfer transaction.
     * @return A list of LedgerEntry instances associated with the transfer.
     */
    List<LedgerEntry> findByTransferId(String transferId);
}
