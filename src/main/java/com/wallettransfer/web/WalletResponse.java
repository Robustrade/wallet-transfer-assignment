package com.wallettransfer.web;

import com.wallettransfer.domain.Wallet;
import java.time.Instant;

/** JSON representation of a {@link Wallet}. */
public record WalletResponse(
    String id, long balance, long openingBalance, Instant createdAt, Instant updatedAt) {

  static WalletResponse from(Wallet wallet) {
    return new WalletResponse(
        wallet.id(),
        wallet.balance(),
        wallet.openingBalance(),
        wallet.createdAt(),
        wallet.updatedAt());
  }
}
