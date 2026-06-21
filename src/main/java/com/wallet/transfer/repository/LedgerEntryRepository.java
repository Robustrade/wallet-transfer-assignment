package com.wallet.transfer.repository;

import com.wallet.transfer.domain.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import java.util.List;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, String> {

    List<LedgerEntry> findByTransferId(String transferId);

    List<LedgerEntry> findByWalletId(String walletId);

    @Query("SELECT SUM(e.amount) FROM LedgerEntry e WHERE e.type = com.wallet.transfer.domain.LedgerEntryType.DEBIT")
    Long sumDebits();

    @Query("SELECT SUM(e.amount) FROM LedgerEntry e WHERE e.type = com.wallet.transfer.domain.LedgerEntryType.CREDIT")
    Long sumCredits();
}
