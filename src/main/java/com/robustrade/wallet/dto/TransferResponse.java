package com.robustrade.wallet.dto;

import com.robustrade.wallet.entities.Transfer;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

public record TransferResponse(
    UUID transferId,
    String status,
    String fromWalletId,
    String toWalletId,
    BigDecimal amount,
    String failureReason,
    Instant createdAt) {

  public static TransferResponse from(Transfer transfer) {
    return new TransferResponse(
        transfer.getId(),
        transfer.getStatus().name(),
        transfer.getFromWalletId(),
        transfer.getToWalletId(),
        transfer.getAmount(),
        transfer.getFailureReason() == null ? null : transfer.getFailureReason().name(),
        transfer.getCreatedAt());
  }
}
