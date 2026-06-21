package com.rajat.wallet.exception;

import java.util.UUID;

/** A referenced wallet does not exist. Mapped to HTTP 404. */
public class WalletNotFoundException extends RuntimeException {

  public WalletNotFoundException(UUID walletId) {
    super("Wallet not found: " + walletId);
  }
}
