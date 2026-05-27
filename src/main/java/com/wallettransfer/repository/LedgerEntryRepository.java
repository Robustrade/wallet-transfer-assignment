package com.wallettransfer.repository;

import com.wallettransfer.domain.LedgerEntry;
import java.util.List;
import org.springframework.data.jpa.repository.JpaRepository;

/**
 * Persistence access for double-entry {@link LedgerEntry} rows.
 */
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

    /**
     * Returns all ledger rows for a transfer (DEBIT and CREDIT), ordered by type name.
     *
     * @param transferId transfer identifier
     * @return ledger entries for the transfer
     */
    List<LedgerEntry> findByTransferIdOrderByTypeAsc(String transferId);
}
