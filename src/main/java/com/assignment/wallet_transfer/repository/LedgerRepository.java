package com.assignment.wallet_transfer.repository;

import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.UUID;

@Repository
public interface LedgerRepository  extends JpaRepository<com.assignment.wallet_transfer.entity.LedgerEntry, UUID> {


}
