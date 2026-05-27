package com.wallettransfer.repository;

import com.wallettransfer.domain.Transfer;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;

/**
 * Persistence access for {@link Transfer} entities.
 */
public interface TransferRepository extends JpaRepository<Transfer, String> {

    /**
     * Finds a transfer by its unique client idempotency key (for replay and retry paths).
     *
     * @param idempotencyKey client-supplied key
     * @return transfer if present
     */
    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}
