package com.example.wallettransfer.application;

public record CreateTransferCommand(
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount) {

    public CreateTransferCommand normalized() {
        return new CreateTransferCommand(
                trim(idempotencyKey),
                trim(fromWalletId),
                trim(toWalletId),
                amount);
    }

    private static String trim(String value) {
        return value == null ? null : value.trim();
    }
}
