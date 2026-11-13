package com.wallettransfer.repository;

import com.wallettransfer.domain.IdempotencyRecord;
import java.util.Optional;

/** Persistence port for idempotency records. */
public interface IdempotencyRepository {

  /**
   * Claims the key for a transfer. If another transaction is claiming the same key right now, this
   * call waits until that transaction commits or rolls back: after a commit the claim is lost,
   * after a rollback it succeeds.
   *
   * @return {@code true} if this call owns the key, {@code false} if it was already claimed
   */
  boolean tryClaim(IdempotencyRecord record);

  Optional<IdempotencyRecord> findByKey(String key);
}
