package com.wallet.api.dto;

import com.wallet.domain.TransferStatus;

public record TransferResponse(
        String transferId,
        TransferStatus status,
        String fromWalletId,
        String toWalletId,
        long amount,
        String failureReason) {}
