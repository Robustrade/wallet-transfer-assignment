package com.rajat.wallet.dto;

import com.rajat.wallet.domain.entities.Wallet;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

/**
 * Outbound contract for a wallet balance read ({@code GET /wallets/{id}}). {@code updatedAt} is the
 * timestamp of the last balance-changing transaction, so a client can tell how fresh the figure is.
 */
public record WalletResponse(
    UUID walletId, BigDecimal balance, String currency, Instant updatedAt) {

  public static WalletResponse from(Wallet wallet) {
    return new WalletResponse(
        wallet.getId(), wallet.getBalance(), wallet.getCurrency(), wallet.getUpdatedAt());
  }
}
