package com.siddharth.wallet.repository;

import com.siddharth.wallet.model.LedgerEntry;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.UUID;

@Repository
public interface LedgerEntryRepository
        extends JpaRepository<LedgerEntry, UUID> {
}