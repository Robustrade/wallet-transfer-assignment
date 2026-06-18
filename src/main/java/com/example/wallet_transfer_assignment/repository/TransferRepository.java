package com.example.wallet_transfer_assignment.repository;


import com.example.wallet_transfer_assignment.entity.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;

import java.util.UUID;

public interface TransferRepository
        extends JpaRepository<Transfer, UUID> {
}