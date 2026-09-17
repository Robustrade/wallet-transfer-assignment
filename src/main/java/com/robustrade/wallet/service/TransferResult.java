package com.robustrade.wallet.service;

import com.robustrade.wallet.entities.Transfer;

/** {@code replayed} is true when this key had already been used for an identical request. */
public record TransferResult(Transfer transfer, boolean replayed) {

  public static TransferResult executed(Transfer transfer) {
    return new TransferResult(transfer, false);
  }

  public static TransferResult replayed(Transfer transfer) {
    return new TransferResult(transfer, true);
  }
}
