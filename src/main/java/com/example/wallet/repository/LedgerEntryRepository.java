package com.example.wallet.repository;

import com.example.wallet.domain.LedgerEntry;
import java.util.List;
import org.springframework.data.jpa.repository.JpaRepository;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

    List<LedgerEntry> findByTransferId(String transferId);

    long countByTransferId(String transferId);
}
