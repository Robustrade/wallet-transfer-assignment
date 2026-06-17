package com.wallettransfer.transfer.repository;

import com.wallettransfer.transfer.domain.Transfer;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface TransferRepository extends JpaRepository<Transfer, UUID> {

    List<Transfer> findAllByOrderByCreatedAtDesc();

    @Query(
            "SELECT t FROM Transfer t WHERE t.fromWalletId = :walletId OR t.toWalletId = :walletId "
                    + "ORDER BY t.createdAt DESC")
    List<Transfer> findByWalletInvolvement(@Param("walletId") String walletId);
}
