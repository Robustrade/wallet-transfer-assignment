package com.rajat.wallet.dto;

import com.rajat.wallet.domain.entities.Transfer;
import com.rajat.wallet.domain.enums.TransferStatus;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.Builder;

/**
 * Outbound contract for a transfer outcome. The same shape is returned for a fresh transfer and for
 * an idempotent replay; {@code status} drives the HTTP code at the controller (PROCESSED -> 201,
 * FAILED -> 422).
 */
@Builder
public record TransferResponse(
    UUID transferId,
    UUID fromWalletId,
    UUID toWalletId,
    BigDecimal amount,
    TransferStatus status,
    String failureReason,
    Instant createdAt) {

  public static TransferResponse from(Transfer transfer) {
    return TransferResponse.builder()
        .transferId(transfer.getId())
        .fromWalletId(transfer.getFromWalletId())
        .toWalletId(transfer.getToWalletId())
        .amount(transfer.getAmount())
        .status(transfer.getStatus())
        .failureReason(transfer.getFailureReason())
        .createdAt(transfer.getCreatedAt())
        .build();
  }
}
