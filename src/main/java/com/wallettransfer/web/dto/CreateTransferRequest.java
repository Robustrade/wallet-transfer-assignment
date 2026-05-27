package com.wallettransfer.web.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Positive;

/**
 * Request body for POST /transfers.
 *
 * @param idempotencyKey client key for exactly-once semantics
 * @param fromWalletId   source wallet id
 * @param toWalletId     destination wallet id
 * @param amount         positive transfer amount
 */
public record CreateTransferRequest(
        @NotBlank String idempotencyKey,
        @NotBlank String fromWalletId,
        @NotBlank String toWalletId,
        @Positive long amount) {}
