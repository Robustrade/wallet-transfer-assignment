package com.wallettransfer.web.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.PositiveOrZero;

/**
 * Request body for POST /wallets.
 *
 * @param id              unique wallet identifier
 * @param initialBalance  starting balance (zero or positive)
 */
public record CreateWalletRequest(@NotBlank String id, @PositiveOrZero long initialBalance) {}
