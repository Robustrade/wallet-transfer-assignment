package com.wallettransfer.service;

import com.wallettransfer.domain.IdempotencyRecord;
import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.LedgerEntryType;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferStatus;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.IdempotencyRecordRepository;
import com.wallettransfer.repository.LedgerEntryRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.service.exception.IdempotencyConflictException;
import com.wallettransfer.service.exception.InvalidRequestException;
import com.wallettransfer.service.exception.NotFoundException;
import com.wallettransfer.web.dto.CreateTransferRequest;
import com.wallettransfer.web.dto.TransferResponse;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Core business logic for wallet-to-wallet transfers.
 * <p>
 * Responsibilities: idempotency, pessimistic locking, double-entry ledger,
 * and safe transfer state transitions (PENDING → PROCESSED / FAILED).
 */
@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;

    /**
     * Creates the service with required persistence dependencies.
     */
    public TransferService(
            TransferRepository transferRepository,
            WalletRepository walletRepository,
            LedgerEntryRepository ledgerEntryRepository,
            IdempotencyRecordRepository idempotencyRecordRepository) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
    }

    /**
     * Creates or replays a transfer for the given idempotency key.
     * <p>
     * Duplicate requests with the same key and payload return the original transfer
     * without debiting again. The entire flow runs in one database transaction.
     *
     * @param request transfer details including idempotency key
     * @return transfer outcome and ledger entries (if processed)
     */
    @Transactional
    public TransferResponse createTransfer(CreateTransferRequest request) {
        validate(request);
        String requestHash = RequestHasher.hash(request);

        // Reserve idempotency slot before creating transfer rows (prevents duplicate side effects).
        reserveIdempotencyRecord(request.idempotencyKey(), requestHash);

        Optional<IdempotencyRecord> idempotencyRecord =
                idempotencyRecordRepository.findById(request.idempotencyKey());
        if (idempotencyRecord.isPresent() && idempotencyRecord.get().getTransferId() != null) {
            // Replay: transfer already linked to this idempotency key.
            Transfer existing = transferRepository
                    .findById(idempotencyRecord.get().getTransferId())
                    .orElseThrow();
            return buildResponse(existing);
        }

        Optional<Transfer> existingTransfer = transferRepository.findByIdempotencyKey(request.idempotencyKey());
        if (existingTransfer.isPresent()) {
            Transfer transfer = existingTransfer.get();
            // Retry path: finish a transfer left in PENDING (e.g. after partial failure).
            if (transfer.getStatus() == TransferStatus.PENDING) {
                processTransfer(transfer);
            }
            return buildResponse(transfer);
        }

        // First-time request: create PENDING transfer, link idempotency record, then process.
        Transfer transfer = newTransfer(request);
        transferRepository.save(transfer);

        IdempotencyRecord record = idempotencyRecordRepository
                .findById(request.idempotencyKey())
                .orElseThrow();
        record.setTransferId(transfer.getId());
        idempotencyRecordRepository.save(record);

        processTransfer(transfer);
        return buildResponse(transfer);
    }

    /**
     * Ensures an idempotency record exists for the key, or validates an existing one.
     * Uses a retry loop to handle concurrent inserts on the same key.
     *
     * @param idempotencyKey client-supplied deduplication key
     * @param requestHash    fingerprint of from/to/amount
     */
    private void reserveIdempotencyRecord(String idempotencyKey, String requestHash) {
        while (true) {
            Optional<IdempotencyRecord> existing = idempotencyRecordRepository.findById(idempotencyKey);
            if (existing.isPresent()) {
                if (!existing.get().getRequestHash().equals(requestHash)) {
                    throw new IdempotencyConflictException(
                            "Idempotency key already used with a different request payload");
                }
                return;
            }
            try {
                idempotencyRecordRepository.save(
                        new IdempotencyRecord(idempotencyKey, requestHash, null, Instant.now()));
                return;
            } catch (DataIntegrityViolationException ignored) {
                // Another thread inserted the same key; loop and re-read.
            }
        }
    }

    /**
     * Executes a PENDING transfer: lock wallets, move balance, write ledger, update status.
     * No-op if the transfer is no longer PENDING (idempotent processing).
     *
     * @param transfer transfer entity to process
     */
    private void processTransfer(Transfer transfer) {
        if (transfer.getStatus() != TransferStatus.PENDING) {
            return;
        }

        try {
            WalletPair wallets = lockWalletsInOrder(transfer.getFromWalletId(), transfer.getToWalletId());
            Wallet from = wallets.from();
            Wallet to = wallets.to();

            if (from.getBalance() < transfer.getAmount()) {
                markFailed(transfer, "insufficient funds");
                return;
            }

            Instant now = Instant.now();
            from.setBalance(from.getBalance() - transfer.getAmount());
            from.setUpdatedAt(now);
            to.setBalance(to.getBalance() + transfer.getAmount());
            to.setUpdatedAt(now);
            walletRepository.save(from);
            walletRepository.save(to);

            persistLedger(transfer, now);
            markProcessed(transfer, now);
        } catch (NotFoundException ex) {
            markFailed(transfer, ex.getMessage());
        }
    }

    /**
     * Acquires pessimistic write locks on both wallets in sorted id order to avoid deadlocks.
     *
     * @param fromWalletId source wallet
     * @param toWalletId   destination wallet
     * @return locked source and destination wallet entities
     */
    private WalletPair lockWalletsInOrder(String fromWalletId, String toWalletId) {
        List<String> ordered = new ArrayList<>(List.of(fromWalletId, toWalletId));
        ordered.sort(Comparator.naturalOrder());
        Wallet from = null;
        Wallet to = null;
        for (String walletId : ordered) {
            Wallet wallet = walletRepository
                    .findByIdForUpdate(walletId)
                    .orElseThrow(() -> new NotFoundException("Wallet not found: " + walletId));
            if (walletId.equals(fromWalletId)) {
                from = wallet;
            } else {
                to = wallet;
            }
        }
        return new WalletPair(from, to);
    }

    /** Holds source and destination wallets after locking. */
    private record WalletPair(Wallet from, Wallet to) {}

    /**
     * Writes exactly one DEBIT and one CREDIT ledger row per transfer (double-entry).
     * Skips insert if entries already exist (safe on retry).
     *
     * @param transfer processed transfer
     * @param now      timestamp for ledger rows
     */
    private void persistLedger(Transfer transfer, Instant now) {
        if (!ledgerEntryRepository.findByTransferIdOrderByTypeAsc(transfer.getId()).isEmpty()) {
            return;
        }
        ledgerEntryRepository.save(new LedgerEntry(
                transfer.getFromWalletId(),
                transfer.getId(),
                LedgerEntryType.DEBIT,
                transfer.getAmount(),
                now));
        ledgerEntryRepository.save(new LedgerEntry(
                transfer.getToWalletId(),
                transfer.getId(),
                LedgerEntryType.CREDIT,
                transfer.getAmount(),
                now));
    }

    /**
     * Marks transfer as PROCESSED if the state machine allows the transition.
     *
     * @param transfer transfer to update
     * @param now      update timestamp
     */
    private void markProcessed(Transfer transfer, Instant now) {
        if (!transfer.getStatus().canTransitionTo(TransferStatus.PROCESSED)) {
            return;
        }
        transfer.setStatus(TransferStatus.PROCESSED);
        transfer.setFailureReason(null);
        transfer.setUpdatedAt(now);
        transferRepository.save(transfer);
    }

    /**
     * Marks transfer as FAILED with a reason; balances are not changed.
     *
     * @param transfer transfer to update
     * @param reason   human-readable failure cause
     */
    private void markFailed(Transfer transfer, String reason) {
        if (!transfer.getStatus().canTransitionTo(TransferStatus.FAILED)) {
            return;
        }
        Instant now = Instant.now();
        transfer.setStatus(TransferStatus.FAILED);
        transfer.setFailureReason(reason);
        transfer.setUpdatedAt(now);
        transferRepository.save(transfer);
    }

    /**
     * Builds a new transfer in PENDING state before processing.
     *
     * @param request validated API request
     * @return unsaved transfer entity with generated id
     */
    private Transfer newTransfer(CreateTransferRequest request) {
        Instant now = Instant.now();
        Transfer transfer = new Transfer();
        transfer.setId(UUID.randomUUID().toString());
        transfer.setIdempotencyKey(request.idempotencyKey());
        transfer.setFromWalletId(request.fromWalletId());
        transfer.setToWalletId(request.toWalletId());
        transfer.setAmount(request.amount());
        transfer.setStatus(TransferStatus.PENDING);
        transfer.setCreatedAt(now);
        transfer.setUpdatedAt(now);
        return transfer;
    }

    /**
     * Maps a transfer entity and its ledger rows to an API response.
     *
     * @param transfer persisted transfer
     * @return API response DTO
     */
    private TransferResponse buildResponse(Transfer transfer) {
        List<LedgerEntry> ledger = ledgerEntryRepository.findByTransferIdOrderByTypeAsc(transfer.getId());
        return TransferResponse.from(transfer, ledger);
    }

    /**
     * Validates transfer request business rules before persistence.
     *
     * @param request incoming transfer request
     * @throws InvalidRequestException if amount or wallets are invalid
     */
    private void validate(CreateTransferRequest request) {
        if (request.amount() <= 0) {
            throw new InvalidRequestException("amount must be positive");
        }
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidRequestException("from and to wallet must differ");
        }
    }
}
