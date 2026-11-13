package com.wallettransfer.domain;

/** The referenced wallet does not exist (maps to HTTP 404). */
public class WalletNotFoundException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public WalletNotFoundException(String walletId) {
    super("wallet not found: " + walletId);
  }
}
