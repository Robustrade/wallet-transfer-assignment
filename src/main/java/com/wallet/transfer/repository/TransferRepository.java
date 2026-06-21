package com.wallet.transfer.repository;

import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferState;
import org.springframework.data.jpa.repository.JpaRepository;
import java.util.List;
import java.util.Optional;

public interface TransferRepository extends JpaRepository<Transfer, String> {

    List<Transfer> findByState(TransferState state);

    List<Transfer> findByFromWalletIdOrToWalletId(String fromWalletId, String toWalletId);

    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}
