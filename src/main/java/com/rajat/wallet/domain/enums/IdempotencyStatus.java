package com.rajat.wallet.domain.enums;

/** Lifecycle of an {@link com.rajat.wallet.domain.entities.IdempotencyRecord}. */
public enum IdempotencyStatus {
  /** Request accepted and still executing; a concurrent duplicate should wait and retry. */
  IN_PROGRESS,
  /** Response captured; duplicate requests are replayed from the cached response. */
  COMPLETED
}
