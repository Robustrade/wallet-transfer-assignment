package com.wallettransfer.service;

/**
 * Request to move money between two wallets.
 *
 * @param idempotencyKey optional; when present a repeated request returns the original result
 */
public record CreateTransferCommand(
    String idempotencyKey, String fromWalletId, String toWalletId, long amount) {}
