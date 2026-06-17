package com.wallettransfer.ledger.repository;

import com.wallettransfer.ledger.domain.LedgerEntry;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

    List<LedgerEntry> findByTransferIdOrderByIdAsc(UUID transferId);

    List<LedgerEntry> findByWalletIdOrderByCreatedAtDesc(String walletId);
}
