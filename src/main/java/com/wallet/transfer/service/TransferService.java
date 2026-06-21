package com.wallet.transfer.service;

import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferStatus;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.dto.TransferResult;
import com.wallet.transfer.exception.IdempotencyKeyConflictException;
import com.wallet.transfer.exception.InsufficientFundsException;
import com.wallet.transfer.exception.InvalidTransferRequestException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionTemplate;

import java.util.Optional;

@Service
public class TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final TransactionTemplate transactionTemplate;

    public TransferService(TransferRepository transferRepository,
                           WalletRepository walletRepository,
                           LedgerEntryRepository ledgerEntryRepository,
                           TransactionTemplate transactionTemplate) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.transactionTemplate = transactionTemplate;
    }

    /**
     * Execute a wallet-to-wallet transfer with idempotency guarantees.
     *
     * This method is intentionally NOT annotated @Transactional. Transaction boundaries are
     * managed explicitly via TransactionTemplate so that DataIntegrityViolationException
     * (idempotency race) can be caught outside the transaction, avoiding a corrupted
     * Hibernate session during recovery.
     *
     * Concurrency strategy:
     * - Idempotency: UNIQUE constraint on idempotency_key. If two requests arrive simultaneously
     *   with the same key, the DB constraint ensures only one wins the insert; the loser's
     *   transaction rolls back and we recover with a fresh lookup.
     * - Payload mismatch: If an idempotency key is reused with different parameters, the service
     *   rejects the request with a 409 CONFLICT to prevent silent data corruption.
     * - Double-spend prevention: SELECT FOR UPDATE on wallet rows in deterministic ID order
     *   prevents deadlocks and serializes concurrent debits on the same wallet.
     * - Atomicity: The entire transfer (debit + credit + ledger + status) executes within
     *   a single database transaction via TransactionTemplate. Failure at any point rolls back.
     * - Race recovery: DataIntegrityViolationException is caught OUTSIDE the transaction boundary,
     *   so the recovery lookup runs in a fresh transaction (no corrupted Hibernate session).
     *
     * Design decision — fast-path idempotency read:
     * The findByIdempotencyKey call below intentionally runs outside a transaction (auto-commit).
     * Under PostgreSQL READ COMMITTED (our target DB), this is safe: it either sees the committed
     * row or doesn't. The UNIQUE constraint inside performTransfer() is the actual correctness
     * guarantee. This fast-path avoids the overhead of starting a full serializable transaction
     * for every duplicate replay request.
     *
     * Design decision — FAILED transfers and idempotency keys:
     * Once a transfer is marked FAILED (e.g. insufficient funds), the idempotency key is permanently
     * consumed. Retrying with the same key will return the original FAILED result. This is intentional:
     * it prevents a scenario where a client retries after depositing funds and inadvertently triggers
     * a transfer that was already observed as failed. Clients should use a new idempotency key for
     * genuinely new transfer attempts.
     */
    public TransferResult executeTransfer(String idempotencyKey, String fromWalletId,
                                          String toWalletId, Long amount) {
        if (fromWalletId.equals(toWalletId)) {
            throw new InvalidTransferRequestException("Source and destination wallets must be different");
        }

        // Step 1: Idempotency fast-path — avoids starting a full transaction for replays.
        // This is an optimization for the common non-contended case; the UNIQUE constraint
        // in performTransfer() is the actual correctness guarantee under races.
        Optional<Transfer> existing = transferRepository.findByIdempotencyKey(idempotencyKey);
        if (existing.isPresent()) {
            Transfer e = existing.get();
            validatePayloadMatch(e, fromWalletId, toWalletId, amount);
            log.info("Duplicate request detected for idempotencyKey={}, returning existing transfer={}",
                    idempotencyKey, e.getId());
            return handleExistingTransfer(e);
        }

        // Step 2: Execute transfer in an explicit transaction
        Transfer transfer;
        try {
            transfer = transactionTemplate.execute(status ->
                    performTransfer(idempotencyKey, fromWalletId, toWalletId, amount));
        } catch (DataIntegrityViolationException e) {
            // Only recover from idempotency key constraint violations.
            // Re-throw other constraint violations (FK, check constraints) as-is.
            if (!isIdempotencyKeyViolation(e)) {
                throw e;
            }
            // Lost the race on idempotency key — transaction already rolled back.
            // Fresh lookup runs in its own transaction (Spring Data default).
            log.info("Idempotency race detected for key={}, returning existing transfer", idempotencyKey);
            Transfer winner = transferRepository.findByIdempotencyKey(idempotencyKey)
                    .orElseThrow(() -> new IllegalStateException(
                            "Concurrent idempotency conflict but transfer not found", e));
            validatePayloadMatch(winner, fromWalletId, toWalletId, amount);
            return handleExistingTransfer(winner);
        }

        // Step 3: If transfer was marked FAILED (insufficient funds), throw after TX commits.
        // The actual balance is persisted in the transfer record for internal audit only — never exposed to clients.
        if (transfer.getStatus() == TransferStatus.FAILED) {
            throw new InsufficientFundsException(fromWalletId, amount);
        }

        log.info("Transfer completed: id={}, from={}, to={}, amount={}",
                transfer.getId(), fromWalletId, toWalletId, amount);

        return new TransferResult(transfer, true);
    }

    private Transfer performTransfer(String idempotencyKey, String fromWalletId,
                                     String toWalletId, Long amount) {
        // Early existence check — avoids acquiring a pessimistic lock on one wallet only to
        // discover the other doesn't exist, which would block concurrent writers unnecessarily.
        if (!walletRepository.existsById(fromWalletId)) {
            throw new WalletNotFoundException(fromWalletId);
        }
        if (!walletRepository.existsById(toWalletId)) {
            throw new WalletNotFoundException(toWalletId);
        }

        // Acquire locks in deterministic order (by wallet ID) to prevent deadlocks
        String firstId = fromWalletId.compareTo(toWalletId) < 0 ? fromWalletId : toWalletId;
        String secondId = fromWalletId.compareTo(toWalletId) < 0 ? toWalletId : fromWalletId;

        Wallet firstLocked = lockWallet(firstId);
        Wallet secondLocked = lockWallet(secondId);

        Wallet sourceWallet = fromWalletId.equals(firstId) ? firstLocked : secondLocked;
        Wallet destWallet = toWalletId.equals(firstId) ? firstLocked : secondLocked;

        // Create transfer record — flush to trigger UNIQUE constraint immediately
        Transfer transfer = new Transfer(idempotencyKey, fromWalletId, toWalletId, amount);
        transferRepository.saveAndFlush(transfer);

        // Check balance before calling debit() — we need to persist a FAILED record
        // rather than throw (debit() would throw BalanceViolationException, preventing persistence).
        if (sourceWallet.getBalance() < amount) {
            transfer.markFailed(sourceWallet.getBalance());
            transferRepository.save(transfer);
            return transfer;
        }

        // Execute balance changes
        sourceWallet.debit(amount);
        destWallet.credit(amount);
        walletRepository.save(sourceWallet);
        walletRepository.save(destWallet);

        // Create double-entry ledger records
        ledgerEntryRepository.save(new LedgerEntry(
                fromWalletId, transfer.getId(), LedgerEntryType.DEBIT, amount));
        ledgerEntryRepository.save(new LedgerEntry(
                toWalletId, transfer.getId(), LedgerEntryType.CREDIT, amount));

        // Mark transfer as processed
        transfer.markProcessed();
        transferRepository.save(transfer);

        return transfer;
    }

    private TransferResult handleExistingTransfer(Transfer transfer) {
        if (transfer.getStatus() == TransferStatus.FAILED) {
            throw new InsufficientFundsException(transfer.getFromWalletId(), transfer.getAmount());
        }
        return new TransferResult(transfer, false);
    }

    private void validatePayloadMatch(Transfer existing, String fromWalletId,
                                       String toWalletId, Long amount) {
        if (!existing.getFromWalletId().equals(fromWalletId)
                || !existing.getToWalletId().equals(toWalletId)
                || !existing.getAmount().equals(amount)) {
            throw new IdempotencyKeyConflictException(existing.getIdempotencyKey());
        }
    }

    private Wallet lockWallet(String walletId) {
        return walletRepository.findByIdForUpdate(walletId)
                .orElseThrow(() -> new WalletNotFoundException(walletId));
    }

    private boolean isIdempotencyKeyViolation(DataIntegrityViolationException e) {
        Throwable cause = e.getMostSpecificCause();
        // PostgreSQL reports unique violations as SQL state 23505.
        // We use the structured constraint name from PSQLException to avoid
        // brittle message-string matching.
        if (cause instanceof org.postgresql.util.PSQLException psqlEx) {
            var serverError = psqlEx.getServerErrorMessage();
            if (serverError == null || !"23505".equals(serverError.getSQLState())) {
                return false;
            }
            return "uq_idempotency_key".equals(serverError.getConstraint());
        }
        // No fallback for generic SQLException — matching on SQL state alone (23505) would
        // incorrectly treat ANY unique violation (e.g. uq_ledger_transfer_wallet_type) as an
        // idempotency race. Since we target PostgreSQL exclusively, the PSQLException branch
        // above is authoritative. Non-PostgreSQL drivers should surface the original exception.
        return false;
    }
}
