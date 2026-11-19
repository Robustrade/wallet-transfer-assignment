package com.wallettransfer.domain;

import java.time.Instant;

/**
 * Moves {@code amount} (minor units) from one wallet to another.
 *
 * <p>Immutable: state transitions return a new instance, and only {@code PENDING} may transition.
 *
 * @param idempotencyKey client supplied key, or {@code null} when none was given
 * @param failureReason set exactly when the state is {@code FAILED}, otherwise {@code null}
 */
public record Transfer(
    String id,
    String idempotencyKey,
    String fromWalletId,
    String toWalletId,
    long amount,
    TransferState state,
    FailureReason failureReason,
    Instant createdAt,
    Instant updatedAt) {

  /** Validates the request and returns a new PENDING transfer. */
  public static Transfer create(
      String id,
      String idempotencyKey,
      String fromWalletId,
      String toWalletId,
      long amount,
      Instant now) {
    if (amount <= 0) {
      throw new ValidationException("amount must be greater than zero");
    }
    if (isBlank(fromWalletId) || isBlank(toWalletId)) {
      throw new ValidationException("source and destination wallet ids are required");
    }
    if (fromWalletId.equals(toWalletId)) {
      throw new ValidationException("source and destination wallets must differ");
    }
    return new Transfer(
        id,
        idempotencyKey,
        fromWalletId,
        toWalletId,
        amount,
        TransferState.PENDING,
        null,
        now,
        now);
  }

  /** PENDING -> PROCESSED. */
  public Transfer processed(Instant now) {
    requirePending(TransferState.PROCESSED);
    return withState(TransferState.PROCESSED, null, now);
  }

  /** PENDING -> FAILED, recording why. */
  public Transfer failed(FailureReason reason, Instant now) {
    requirePending(TransferState.FAILED);
    return withState(TransferState.FAILED, reason, now);
  }

  private void requirePending(TransferState target) {
    if (state != TransferState.PENDING) {
      throw new InvalidStateTransitionException(state, target);
    }
  }

  private Transfer withState(TransferState newState, FailureReason reason, Instant now) {
    return new Transfer(
        id, idempotencyKey, fromWalletId, toWalletId, amount, newState, reason, createdAt, now);
  }

  private static boolean isBlank(String value) {
    return value == null || value.isBlank();
  }
}
