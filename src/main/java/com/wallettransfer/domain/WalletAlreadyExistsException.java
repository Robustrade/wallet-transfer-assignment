package com.wallettransfer.domain;

/** A wallet with this id already exists (maps to HTTP 409). */
public class WalletAlreadyExistsException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public WalletAlreadyExistsException(String walletId) {
    this(walletId, null);
  }

  public WalletAlreadyExistsException(String walletId, Throwable cause) {
    super("wallet already exists: " + walletId, cause);
  }
}
