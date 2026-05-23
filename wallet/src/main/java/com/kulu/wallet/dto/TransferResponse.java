package com.kulu.wallet.dto;

public record TransferResponse(
    String transferId,
    String status,
    String failureReason,
    long fromWalletId,
    long toWalletId,
    long amount
) {}
