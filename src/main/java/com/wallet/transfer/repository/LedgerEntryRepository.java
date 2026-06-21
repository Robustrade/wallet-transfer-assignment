package com.wallet.transfer.repository;

import com.wallet.transfer.domain.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.List;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

    List<LedgerEntry> findByTransferId(String transferId);

    List<LedgerEntry> findByWalletIdOrderByCreatedAtDesc(String walletId);
}
