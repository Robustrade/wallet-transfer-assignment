package com.wallettransfer.web;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.wallettransfer.domain.Transfer;
import java.time.Instant;

/** JSON representation of a {@link Transfer}. */
@JsonInclude(JsonInclude.Include.NON_NULL)
public record TransferResponse(
    String id,
    String idempotencyKey,
    String fromWalletId,
    String toWalletId,
    long amount,
    String state,
    String failureReason,
    Instant createdAt,
    Instant updatedAt) {

  static TransferResponse from(Transfer transfer) {
    return new TransferResponse(
        transfer.id(),
        transfer.idempotencyKey(),
        transfer.fromWalletId(),
        transfer.toWalletId(),
        transfer.amount(),
        transfer.state().name(),
        transfer.failureReason() == null ? null : transfer.failureReason().name(),
        transfer.createdAt(),
        transfer.updatedAt());
  }
}
