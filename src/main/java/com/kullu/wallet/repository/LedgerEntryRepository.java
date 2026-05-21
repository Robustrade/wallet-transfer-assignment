package com.kullu.wallet.repository;

import java.util.List;
import java.util.UUID;

import org.springframework.data.jpa.repository.JpaRepository;

import com.kullu.wallet.entity.LedgerEntry;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

    List<LedgerEntry> findAllByTransferId(UUID transferId);

    List<LedgerEntry> findAllByWalletIdOrderByCreatedAtDesc(String walletId);
}
