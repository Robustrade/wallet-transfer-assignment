package com.kullu.wallet.repository;

import java.util.UUID;

import org.springframework.data.jpa.repository.JpaRepository;

import com.kullu.wallet.entity.TransferEntity;

public interface TransferRepository extends JpaRepository<TransferEntity, UUID> {
}
