package com.rajat.wallet.dto;

import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.Digits;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;
import java.math.BigDecimal;
import java.util.UUID;

/**
 * Inbound contract for {@code POST /transfers}. Field-level constraints are transport validation (a
 * malformed request can never reach the service); business rules (funds, wallet existence) are
 * enforced downstream. Violations are mapped to HTTP 400 by {@code GlobalExceptionHandler}.
 */
public record CreateTransferRequest(
    @NotBlank String idempotencyKey,
    @NotNull UUID fromWalletId,
    @NotNull UUID toWalletId,
    @NotNull @Positive @Digits(integer = 17, fraction = 2) BigDecimal amount) {

  /** Mirrors the DB {@code from <> to} CHECK so a self-transfer is rejected before any work. */
  @AssertTrue(message = "fromWalletId and toWalletId must be different")
  public boolean isWalletsDistinct() {
    return fromWalletId == null || !fromWalletId.equals(toWalletId);
  }
}
