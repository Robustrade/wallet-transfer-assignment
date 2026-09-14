package com.robustrade.wallet.repository;

import com.robustrade.wallet.domain.LedgerEntry;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, UUID> {

    List<LedgerEntry> findByWalletId(String walletId);

    List<LedgerEntry> findByWalletIdOrderByCreatedAtAscIdAsc(String walletId);

    List<LedgerEntry> findByTransferId(UUID transferId);
}
