package com.wallet.transfer.repository;

import com.wallet.transfer.entity.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TransferRepository extends JpaRepository<Transfer, String> {
}
