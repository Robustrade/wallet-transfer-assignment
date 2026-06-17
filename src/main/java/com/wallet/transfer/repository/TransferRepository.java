package com.wallet.transfer.repository;

import com.wallet.transfer.model.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;

/**
 * Repository interface for managing {@link Transfer} entity persistence.
 * Provides standard CRUD operations and custom query methods for transfer transactions.
 */
public interface TransferRepository extends JpaRepository<Transfer, String> {

    /**
     * Finds a transfer record by its unique idempotency key.
     * This is useful for checking if a request has been processed before.
     *
     * @param idempotencyKey The unique key identifying the transfer request.
     * @return An Optional containing the Transfer record if found, or empty otherwise.
     */
    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}
