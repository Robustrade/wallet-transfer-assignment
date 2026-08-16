package com.api.wallet.repository;

import com.api.wallet.domain.Transfer;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TransferRepository extends JpaRepository<Transfer, UUID> {

    List<Transfer> findByFromWalletIdOrToWalletIdOrderByCreatedAtDesc(String fromWalletId, String toWalletId);
}
