package com.wallettransfer.web;

import com.wallettransfer.domain.ValidationException;

/**
 * JSON body of {@code POST /wallets}. {@code openingBalance} is boxed so that a missing field can
 * be told apart from zero.
 */
public record CreateWalletRequest(String id, Long openingBalance) {

  long requireOpeningBalance() {
    if (openingBalance == null) {
      throw new ValidationException("openingBalance is required");
    }
    return openingBalance;
  }
}
