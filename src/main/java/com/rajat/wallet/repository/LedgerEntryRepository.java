package com.rajat.wallet.repository;

import com.rajat.wallet.domain.entities.LedgerEntry;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, UUID> {}
