package com.wallet.transfer.repository;

import com.wallet.transfer.entity.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, String> {
}
