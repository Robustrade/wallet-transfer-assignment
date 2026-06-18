package com.videtech.wallet_transfer.repository;

import java.util.List;

import org.springframework.data.jpa.repository.JpaRepository;

import com.videtech.wallet_transfer.domain.LedgerEntry;

public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, String> {

    List<LedgerEntry> findByTransferId(String transferId);
}
