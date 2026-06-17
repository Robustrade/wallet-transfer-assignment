package com.example.wallettransfer.application;

import com.example.wallettransfer.domain.TransferState;

import java.util.UUID;

public record TransferResult(
        UUID transferId,
        TransferState state,
        String fromWalletId,
        String toWalletId,
        long amount,
        String failureReason) {
}
