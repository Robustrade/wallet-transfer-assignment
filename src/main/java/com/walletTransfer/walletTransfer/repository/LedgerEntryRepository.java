package com.walletTransfer.walletTransfer.repository;

import com.walletTransfer.walletTransfer.entity.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.UUID;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, UUID> {
    // Custom query methods can be defined here if needed
}
