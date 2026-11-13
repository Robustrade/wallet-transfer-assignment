package com.wallettransfer.domain;

import java.time.Instant;

/**
 * A wallet with a stored balance in minor units.
 *
 * <p>The balance never goes below zero and always equals {@code openingBalance} plus all credits
 * minus all debits recorded in the ledger.
 */
public record Wallet(
    String id, long balance, long openingBalance, Instant createdAt, Instant updatedAt) {

  /** Validates the input and returns a wallet funded with {@code openingBalance}. */
  public static Wallet open(String id, long openingBalance, Instant now) {
    if (id == null || id.isBlank()) {
      throw new ValidationException("wallet id is required");
    }
    if (openingBalance < 0) {
      throw new ValidationException("opening balance must not be negative");
    }
    return new Wallet(id, openingBalance, openingBalance, now, now);
  }
}
