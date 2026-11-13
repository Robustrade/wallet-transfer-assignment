package com.wallettransfer.domain;

/** The referenced transfer does not exist (maps to HTTP 404). */
public class TransferNotFoundException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public TransferNotFoundException(String transferId) {
    super("transfer not found: " + transferId);
  }
}
