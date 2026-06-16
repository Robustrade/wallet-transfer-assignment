package com.wallet.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.controller.dto.TransferResponse;
import com.wallet.domain.EntryType;
import com.wallet.domain.IdempotencyStatus;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.IdempotencyKeyInProgressException;
import com.wallet.exception.InsufficientBalanceException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.metrics.TransferMetrics;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.util.Hashing;
import com.wallet.util.LogSafe;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionSynchronization;
import org.springframework.transaction.support.TransactionSynchronizationManager;

/**
 * Transactional core of a transfer: claim the idempotency key, lock both
 * wallets, debit/credit, write ledger entries, finalize the idempotency record.
 *
 * <p>No {@code noRollbackFor} — any exception rolls back the whole transaction
 * (including the IN_PROGRESS row). The outer {@link TransferService} catches
 * business exceptions and persists FAILED state in a fresh transaction via
 * {@link IdempotencyFailureRecorder}.
 */
@Service
public class TransferExecutionService {

    private static final Logger log = LoggerFactory.getLogger(TransferExecutionService.class);

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final ObjectMapper objectMapper;
    private final TransferMetrics metrics;

    public TransferExecutionService(WalletRepository walletRepository,
                                    TransferRepository transferRepository,
                                    LedgerEntryRepository ledgerEntryRepository,
                                    IdempotencyRecordRepository idempotencyRecordRepository,
                                    ObjectMapper objectMapper,
                                    TransferMetrics metrics) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.objectMapper = objectMapper;
        this.metrics = metrics;
    }

    /**
     * Runs the full transfer pipeline in a single transaction. Two primitives
     * guard against duplicates: an advisory lock fast-rejects in-flight retries,
     * and {@code INSERT ... ON CONFLICT} provides durable exactly-once claim
     * across process restarts.
     */
    @Transactional(
            isolation = Isolation.READ_COMMITTED,
            timeoutString = "${transfer.transaction.timeout-seconds:10}"
    )
    public TransferService.TransferResult tryExecuteTransfer(String clientId, String idempotencyKey,
                                                             CreateTransferRequest request, String requestHash) {
        log.info("Processing transfer inside transaction: client={}, idempotencyKey={}, from={}, to={}, amount={}",
                clientId, LogSafe.mask(idempotencyKey), request.fromWalletId(), request.toWalletId(), request.amount());

        guardConcurrentRequest(clientId, idempotencyKey);
        claimIdempotencyKey(clientId, idempotencyKey, requestHash);

        LockedWallets locked = lockBothWallets(request);
        ensureSufficientBalance(request, locked.source());

        Transfer transfer = executeBalancedTransfer(request, locked.source(), locked.dest());
        return finalizeIdempotency(clientId, idempotencyKey, transfer);
    }

    /**
     * Fast-rejects concurrent requests for the same key via a transaction-scoped
     * advisory lock. Increments {@code idempotency.advisory_lock.rejections} on
     * rejection — almost always a true concurrent retry, but a sustained
     * non-zero rate under low traffic would indicate
     * {@link Hashing#toLockId(String)} collisions, where two unrelated keys
     * fold to the same 64-bit lock id and one would see a spurious 409.
     * Durable correctness is preserved by the {@code INSERT ... ON CONFLICT}
     * claim; the metric only exists to make the false-positive case observable.
     */
    private void guardConcurrentRequest(String clientId, String idempotencyKey) {
        long lockId = Hashing.toLockId(clientId + ":" + idempotencyKey);
        if (!idempotencyRecordRepository.tryAdvisoryXactLock(lockId)) {
            metrics.recordAdvisoryLockRejection();
            log.info("concurrent idempotency key lock request rejected: client={}, key={}",
                    clientId, LogSafe.mask(idempotencyKey));
            // Debug-level discriminator: when the rejections counter ticks up
            // at low traffic (the documented signal that toLockId is folding
            // unrelated keys to the same 64-bit id), a TRACE/DEBUG capture
            // surfaces the lockId so an oncall can correlate two rejections
            // back to the same hash bucket. Kept at debug because lockId on
            // every info line would burn space in the steady state where the
            // rejection is just a true concurrent retry.
            if (log.isDebugEnabled()) {
                log.debug("idempotency advisory-lock collision detail: client={}, key={}, lockId={}",
                        clientId, LogSafe.mask(idempotencyKey), lockId);
            }
            throw new IdempotencyKeyInProgressException(idempotencyKey);
        }
    }

    /**
     * Durably claims the (clientId, key) tuple. Catches conflicts with
     * previously-completed keys that the in-memory advisory lock cannot see.
     */
    private void claimIdempotencyKey(String clientId, String idempotencyKey, String requestHash) {
        int rowsInserted = idempotencyRecordRepository.insertIdempotencyRecord(
                clientId, idempotencyKey, requestHash, IdempotencyStatus.IN_PROGRESS.name());
        if (rowsInserted == 0) {
            log.info("duplicate idempotency key detected: client={}, key={}",
                    clientId, LogSafe.mask(idempotencyKey));
            throw new IdempotencyConflictException(idempotencyKey);
        }
    }

    /**
     * Locks both wallets {@code FOR UPDATE} in deterministic ID order to avoid
     * A→B / B→A deadlocks. Missing wallets throw and roll back.
     */
    private LockedWallets lockBothWallets(CreateTransferRequest request) {
        String firstId;
        String secondId;
        if (request.fromWalletId().compareTo(request.toWalletId()) < 0) {
            firstId = request.fromWalletId();
            secondId = request.toWalletId();
        } else {
            firstId = request.toWalletId();
            secondId = request.fromWalletId();
        }

        Wallet first = walletRepository.findByIdForUpdate(firstId)
                .orElseThrow(() -> new WalletNotFoundException(firstId));
        Wallet second = walletRepository.findByIdForUpdate(secondId)
                .orElseThrow(() -> new WalletNotFoundException(secondId));

        Wallet source = firstId.equals(request.fromWalletId()) ? first : second;
        Wallet dest = firstId.equals(request.toWalletId()) ? first : second;
        return new LockedWallets(source, dest);
    }

    private void ensureSufficientBalance(CreateTransferRequest request, Wallet source) {
        if (source.getBalance() < request.amount()) {
            throw new InsufficientBalanceException(request.fromWalletId(),
                    source.getBalance(), request.amount());
        }
    }

    /**
     * Inserts the Transfer directly in PROCESSED state and writes the balanced
     * debit/credit ledger pair. The whole sequence commits atomically.
     *
     * <p><strong>Single-transaction invariant.</strong> Every statement in
     * this body — Transfer INSERT, two wallet UPDATEs, two ledger INSERTs —
     * MUST execute within the {@code @Transactional} boundary opened on
     * {@link #tryExecuteTransfer}. Splitting any of them out (a separate
     * transaction, an async post-commit hook, a cross-shard fan-out, anything
     * that gives a statement its own commit lifecycle) silently breaks the
     * all-or-nothing property: a partial failure could leave a debit without
     * its credit, or a Transfer row with no ledger entries. The class-level
     * Javadoc states this once for the whole pipeline; this method-local note
     * pins it in the place a future refactor is most likely to start.
     *
     * <p>{@code running_balance} on each ledger row is taken from the post-UPDATE
     * balance returned by {@code walletRepository.save(...)} rather than the
     * in-memory wallet — the DB is the single source of truth, making the
     * snapshot robust to any future reordering that lets the in-memory wallet
     * drift from the persisted row.
     */
    private Transfer executeBalancedTransfer(CreateTransferRequest request, Wallet source, Wallet dest) {
        Transfer transfer = Transfer.processed(request.fromWalletId(), request.toWalletId(), request.amount());
        // Reassign — the repository returns the row with DB-assigned timestamps;
        // the cached idempotency response embeds these values.
        transfer = transferRepository.insert(transfer);

        source.debit(request.amount());
        Wallet persistedSource = walletRepository.save(source);

        dest.credit(request.amount());
        Wallet persistedDest = walletRepository.save(dest);

        LedgerEntry debitEntry = new LedgerEntry(
                request.fromWalletId(), transfer.getId(), EntryType.DEBIT, request.amount(),
                persistedSource.getBalance());
        LedgerEntry creditEntry = new LedgerEntry(
                request.toWalletId(), transfer.getId(), EntryType.CREDIT, request.amount(),
                persistedDest.getBalance());
        ledgerEntryRepository.save(debitEntry);
        ledgerEntryRepository.save(creditEntry);

        log.info("Transfer completed successfully: id={}, from={}, to={}, amount={}",
                transfer.getId(), request.fromWalletId(), request.toWalletId(), request.amount());
        return transfer;
    }

    /**
     * Caches the success response on the idempotency record. The success
     * counter increments only on commit (via {@link TransactionSynchronization})
     * so a post-update rollback does not double-count.
     */
    private TransferService.TransferResult finalizeIdempotency(String clientId, String idempotencyKey,
                                                               Transfer transfer) {
        TransferResponse response = TransferResponse.from(transfer);
        String responseJson = serializeResponse(response);

        int rowsUpdated = idempotencyRecordRepository.markCompleted(
                clientId, idempotencyKey, transfer.getId(), HttpStatus.CREATED.value(), responseJson);
        if (rowsUpdated == 0) {
            throw new IllegalStateException(
                    "Idempotency record not found for client: " + clientId + ", key: " + idempotencyKey);
        }
        recordCreatedAfterCommit();
        return new TransferService.TransferResult.Fresh(response, HttpStatus.CREATED);
    }

    private void recordCreatedAfterCommit() {
        if (TransactionSynchronizationManager.isSynchronizationActive()) {
            TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
                @Override
                public void afterCommit() {
                    metrics.recordCreated();
                }
            });
        } else {
            metrics.recordCreated();
        }
    }

    private String serializeResponse(TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JsonProcessingException e) {
            throw new RuntimeException("Failed to serialize response", e);
        }
    }

    private record LockedWallets(Wallet source, Wallet dest) {}
}
