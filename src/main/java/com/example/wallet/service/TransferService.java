package com.example.wallet.service;

import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.TransferResponse;

public interface TransferService {

    /**
     * Execute a wallet-to-wallet transfer.
     *
     * Guarantees:
     * - Idempotent: same idempotencyKey always returns the same result.
     * - Atomic: all side effects (balance update, ledger entries) happen in one transaction.
     * - Safe under concurrency: row-level locks prevent double-spending.
     *
     * @param request the transfer request containing idempotencyKey, wallets, and amount
     * @return the transfer response (newly created or replayed from idempotency cache)
     */
    TransferResponse execute(CreateTransferRequest request);
}

