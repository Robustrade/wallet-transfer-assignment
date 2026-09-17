package com.robustrade.wallet.dto;

import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;
import java.math.BigDecimal;

public record CreateTransferRequest(
    @NotBlank @Size(max = 255) String idempotencyKey,
    @NotBlank String fromWalletId,
    @NotBlank String toWalletId,
    @NotNull @DecimalMin(value = "0", inclusive = false) BigDecimal amount) {

  /** Caught here as a 400 rather than letting the DB CHECK constraint surface as a 500. */
  @AssertTrue(message = "must differ from fromWalletId")
  public boolean isToWalletIdDifferent() {
    return fromWalletId == null || !fromWalletId.equals(toWalletId);
  }
}
