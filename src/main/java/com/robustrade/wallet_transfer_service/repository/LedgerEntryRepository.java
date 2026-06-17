package com.robustrade.wallet_transfer_service.repository;

import com.robustrade.wallet_transfer_service.entity.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {
}