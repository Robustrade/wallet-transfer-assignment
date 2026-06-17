package com.assignment.wallet_transfer.repository;


import com.assignment.wallet_transfer.entity.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;
import java.util.UUID;


@Repository
public interface TransferRepository extends JpaRepository<com.assignment.wallet_transfer.entity.Transfer, UUID> {

    Optional<com.assignment.wallet_transfer.entity.Transfer> findByIdempotencyKey(String idempotencyKey);

    List<Transfer> findBySourceWalletIdOrDestinationWalletId(
            UUID sourceWalletId,
            UUID destinationWalletId
    );

}