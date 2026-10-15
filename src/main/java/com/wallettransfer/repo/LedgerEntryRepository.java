package com.wallettransfer.repo;

import com.wallettransfer.entity.LedgerEntry;
import java.util.List;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

@Repository
public interface LedgerEntryRepository extends JpaRepository<LedgerEntry, Long> {

  List<LedgerEntry> findByTransfer_Id(String transferId);

  List<LedgerEntry> findByWallet_WalletIdOrderByCreatedAtDesc(String walletId);
}
