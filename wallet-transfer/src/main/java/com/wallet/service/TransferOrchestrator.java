package com.wallet.service;

import com.wallet.domain.LedgerEntry;
import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import java.util.List;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

/**
 * Handles transactional phases of the transfer workflow.
 *
 * <p>Extracted into a separate Spring bean so that {@code @Transactional} methods are called
 * through the Spring AOP proxy, making propagation semantics (REQUIRES_NEW, REQUIRED) effective.
 * Calling {@code @Transactional} methods within the same class bypasses the proxy entirely.
 */
@Service
public class TransferOrchestrator {

    private static final Logger log = LoggerFactory.getLogger(TransferOrchestrator.class);

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    public TransferOrchestrator(
        TransferRepository transferRepository,
        WalletRepository walletRepository,
        LedgerEntryRepository ledgerEntryRepository) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    /**
     * Phase 1: Commit a PENDING transfer record in its own transaction before processing begins.
     *
     * <p>REQUIRES_NEW ensures the PENDING record is durable and visible to other requests before
     * Phase 2 starts. This means retries that arrive while processing is in-flight see the PENDING
     * state and return it instead of creating a duplicate.
     *
     * <p>The unique index on {@code idempotency_key} handles the concurrent duplicate race: if two
     * requests pass the {@code findByIdempotencyKey} check simultaneously, only one INSERT succeeds.
     * The loser catches the constraint violation and re-fetches the winner's record.
     */
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public IdempotencyOutcome getOrCreatePendingTransfer(TransferRequest request) {
        return transferRepository.findByIdempotencyKey(request.idempotencyKey())
            .map(existing -> new IdempotencyOutcome(existing, true))
            .orElseGet(() -> {
                try {
                    Transfer transfer = Transfer.pending(
                        request.idempotencyKey(),
                        request.fromWalletId(),
                        request.toWalletId(),
                        request.amount()
                    );
                    Transfer saved = transferRepository.saveAndFlush(transfer);
                    return new IdempotencyOutcome(saved, false);
                } catch (DataIntegrityViolationException ex) {
                    // Concurrent request with same key committed first — return its record
                    Transfer existing = transferRepository
                        .findByIdempotencyKey(request.idempotencyKey())
                        .orElseThrow(() -> new IllegalStateException(
                            "Idempotency constraint violated but record not found", ex));
                    return new IdempotencyOutcome(existing, true);
                }
            });
    }

    /**
     * Phase 2: Execute the transfer atomically.
     *
     * <p>Acquires pessimistic write locks ({@code SELECT FOR UPDATE}) on both wallet rows in
     * ascending ID order to prevent deadlocks when two transfers debit the same pair of wallets
     * in opposite directions. All balance updates and ledger entries commit in a single transaction.
     */
    @Transactional
    public TransferResponse processTransfer(Transfer pendingTransfer) {
        List<String> lockOrder = List.of(
            pendingTransfer.getFromWalletId(),
            pendingTransfer.getToWalletId()
        ).stream().sorted().toList();

        Wallet first = walletRepository.findByIdForUpdate(lockOrder.get(0))
            .orElseThrow(() -> new WalletNotFoundException(lockOrder.get(0)));
        Wallet second = walletRepository.findByIdForUpdate(lockOrder.get(1))
            .orElseThrow(() -> new WalletNotFoundException(lockOrder.get(1)));

        Wallet fromWallet = first.getId().equals(pendingTransfer.getFromWalletId()) ? first : second;
        Wallet toWallet = first.getId().equals(pendingTransfer.getToWalletId()) ? first : second;

        fromWallet.debit(pendingTransfer.getAmount());
        toWallet.credit(pendingTransfer.getAmount());

        // Dirty checking will flush these on commit; explicit save for clarity
        walletRepository.save(fromWallet);
        walletRepository.save(toWallet);

        ledgerEntryRepository.save(
            LedgerEntry.debit(fromWallet.getId(), pendingTransfer.getId(), pendingTransfer.getAmount())
        );
        ledgerEntryRepository.save(
            LedgerEntry.credit(toWallet.getId(), pendingTransfer.getId(), pendingTransfer.getAmount())
        );

        // Re-fetch pendingTransfer: it was saved in a different (REQUIRES_NEW) transaction
        // and is a detached entity in this transaction context
        Transfer transfer = transferRepository.findById(pendingTransfer.getId())
            .orElseThrow(() -> new IllegalStateException("Transfer not found in Phase 2: " + pendingTransfer.getId()));
        transfer.markProcessed();
        transferRepository.save(transfer);

        log.info("Transfer {} processed: {} -> {} amount={}",
            transfer.getId(), transfer.getFromWalletId(), transfer.getToWalletId(), transfer.getAmount());

        return TransferResponse.from(transfer);
    }

    /**
     * Phase 3 (compensation): Persist FAILED state in its own transaction.
     *
     * <p>REQUIRES_NEW ensures this commit succeeds independently of the rolled-back Phase 2
     * transaction. Without this, the FAILED state would be lost when Phase 2 rolls back.
     */
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void markTransferFailed(UUID transferId, String reason) {
        transferRepository.findById(transferId).ifPresent(transfer -> {
            if (transfer.isPending()) {
                transfer.markFailed(reason);
                transferRepository.save(transfer);
                log.warn("Transfer {} marked FAILED: {}", transferId, reason);
            }
        });
    }

    public record IdempotencyOutcome(Transfer transfer, boolean isDuplicate) {}
}
