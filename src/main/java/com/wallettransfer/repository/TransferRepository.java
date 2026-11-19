package com.wallettransfer.repository;

import com.wallettransfer.domain.InvalidStateTransitionException;
import com.wallettransfer.domain.Transfer;
import java.util.Optional;

/** Persistence port for transfers. */
public interface TransferRepository {

  void insert(Transfer transfer);

  Optional<Transfer> findById(String id);

  /**
   * Persists a state transition made on the in-memory transfer. The write is guarded with {@code
   * state = 'PENDING'}, so a stale or duplicated worker cannot overwrite a terminal state.
   *
   * @throws InvalidStateTransitionException if the stored transfer is no longer PENDING
   */
  void updateState(Transfer transfer);
}
