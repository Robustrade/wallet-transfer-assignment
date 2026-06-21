package com.rajat.wallet.domain.enums;

/** Lifecycle of a transfer. Allowed transitions: PENDING -> PROCESSED, PENDING -> FAILED. */
public enum TransferStatus {
  PENDING,
  PROCESSED,
  FAILED
}
