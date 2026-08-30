package com.example.wallettransfer.repository;

import com.example.wallettransfer.domain.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.domain.Pageable;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.time.Instant;
import java.util.List;
import java.util.UUID;

public interface TransferRepository extends JpaRepository<Transfer, UUID> {

    @Query("""
            select t from Transfer t
            where (t.fromWalletId = :walletId or t.toWalletId = :walletId)
            order by t.createdAt desc, t.id desc
            """)
    List<Transfer> findFirstHistory(@Param("walletId") String walletId, Pageable pageable);

    @Query("""
            select t from Transfer t
            where (t.fromWalletId = :walletId or t.toWalletId = :walletId)
              and (t.createdAt < :createdAt or (t.createdAt = :createdAt and t.id < :transferId))
            order by t.createdAt desc, t.id desc
            """)
    List<Transfer> findHistoryAfter(
            @Param("walletId") String walletId,
            @Param("createdAt") Instant createdAt,
            @Param("transferId") UUID transferId,
            Pageable pageable
    );
}
