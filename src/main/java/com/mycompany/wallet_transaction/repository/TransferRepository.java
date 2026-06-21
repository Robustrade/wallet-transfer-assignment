package com.mycompany.wallet_transaction.repository;

import com.mycompany.wallet_transaction.entities.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TransferRepository extends JpaRepository<Transfer, Long> {
}
