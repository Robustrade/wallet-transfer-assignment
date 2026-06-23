package com.example.wallet.repository;

import com.example.wallet.domain.Transfer;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;

public interface TransferRepository extends JpaRepository<Transfer, String> {

    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}
