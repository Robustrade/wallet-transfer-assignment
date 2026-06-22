package com.walletTransfer.walletTransfer.repository;

import com.walletTransfer.walletTransfer.entity.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.UUID;

public interface TransferRepository  extends JpaRepository<Transfer, UUID> {
}
