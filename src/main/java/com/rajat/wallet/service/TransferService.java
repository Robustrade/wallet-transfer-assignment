package com.rajat.wallet.service;

import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;

/**
 * Orchestrates wallet-to-wallet transfers with exactly-once semantics. The contract:
 *
 * <ul>
 *   <li>A first-seen {@code idempotencyKey} executes the transfer atomically and returns the
 *       outcome (PROCESSED, or FAILED for a recorded business failure such as insufficient funds).
 *   <li>A duplicate of a completed request returns the original result (replay) without repeating
 *       any side effect.
 *   <li>A key reused with a different payload, or one whose original request is still in flight,
 *       raises {@link com.rajat.wallet.exception.IdempotencyConflictException}.
 *   <li>An unknown wallet raises {@link com.rajat.wallet.exception.WalletNotFoundException}.
 * </ul>
 */
public interface TransferService {

  TransferResponse createTransfer(CreateTransferRequest request);
}
