package com.example.wallet.service;

public record CreateTransferCommand(String idempotencyKey, String fromWalletId, String toWalletId, long amount) {}
