package com.wallettransfer.web;

import com.wallettransfer.domain.ValidationException;
import com.wallettransfer.service.CreateTransferCommand;

/**
 * JSON body of {@code POST /transfers}. {@code amount} is boxed so that a missing field can be told
 * apart from zero.
 */
public record CreateTransferRequest(
    String idempotencyKey, String fromWalletId, String toWalletId, Long amount) {

  CreateTransferCommand toCommand() {
    if (amount == null) {
      throw new ValidationException("amount is required");
    }
    return new CreateTransferCommand(idempotencyKey, fromWalletId, toWalletId, amount);
  }
}
