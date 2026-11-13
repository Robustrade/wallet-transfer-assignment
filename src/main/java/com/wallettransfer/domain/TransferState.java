package com.wallettransfer.domain;

/** Lifecycle state of a transfer. PENDING may move to PROCESSED or FAILED; both are terminal. */
public enum TransferState {
  PENDING,
  PROCESSED,
  FAILED
}
