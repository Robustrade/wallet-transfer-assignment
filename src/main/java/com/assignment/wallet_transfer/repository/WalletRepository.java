package com.assignment.wallet_transfer.repository;


import jakarta.persistence.Id;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.UUID;

@Repository
public interface WalletRepository extends JpaRepository<com.assignment.wallet_transfer.entity.Wallet, UUID> {
}
