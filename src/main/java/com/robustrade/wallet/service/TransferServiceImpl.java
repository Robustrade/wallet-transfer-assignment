package com.robustrade.wallet.service;

import com.robustrade.wallet.domain.IdempotencyRecord;
import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.metrics.TransferMetricOutcome;
import com.robustrade.wallet.metrics.TransferMetrics;
import com.robustrade.wallet.repository.IdempotencyRecordRepository;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.TransferRepository;
import com.robustrade.wallet.repository.WalletRepository;

import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

import io.micrometer.observation.Observation;
import io.micrometer.observation.ObservationRegistry;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionTemplate;

@Slf4j
@Service
@RequiredArgsConstructor
public class TransferServiceImpl implements TransferService {

    private static final String INSUFFICIENT_BALANCE = "Insufficient balance";

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final TransferStatusService transferStatusService;
    private final TransactionTemplate transactionTemplate;
    private final TransferMetrics transferMetrics;
    private final ObservationRegistry observationRegistry;

    @Override
    public TransferResponse createTransfer(CreateTransferRequest request) {
        return Observation.createNotStarted("wallet.transfer", observationRegistry)
                .lowCardinalityKeyValue("operation", "create")
                .highCardinalityKeyValue("idempotencyKey", request.getIdempotencyKey())
                .highCardinalityKeyValue("fromWalletId", request.getFromWalletId())
                .highCardinalityKeyValue("toWalletId", request.getToWalletId())
                .observe(() -> transferMetrics.track(() -> createTransferInternal(request)));
    }

    private TransferResponse createTransferInternal(CreateTransferRequest request) {
        log.info(
                "Processing transfer: idempotencyKey={}, fromWalletId={}, toWalletId={}, amount={}",
                request.getIdempotencyKey(),
                request.getFromWalletId(),
                request.getToWalletId(),
                request.getAmount()
        );

        try {
            ensureWalletsExist(request.getFromWalletId(), request.getToWalletId());

            String requestHash = RequestHash.from(
                    request.getFromWalletId(),
                    request.getToWalletId(),
                    request.getAmount()
            );
            log.debug(
                    "Computed requestHash={} for idempotencyKey={}",
                    requestHash,
                    request.getIdempotencyKey()
            );

            Optional<IdempotencyRecord> existing =
                    idempotencyRecordRepository.findByIdempotencyKey(request.getIdempotencyKey());

            if (existing.isPresent()) {
                log.info(
                        "Idempotency key already exists: idempotencyKey={}, transferId={}",
                        request.getIdempotencyKey(),
                        existing.get().getTransferId()
                );
                return handleExistingIdempotency(existing.get(), requestHash);
            }

            log.debug("No existing idempotency record; creating PENDING transfer");
            Optional<Transfer> created = createPendingTransferWithIdempotency(request, requestHash);
            if (created.isEmpty()) {
                // Lost unique(idempotency_key) race: reload winner and follow replay/resume path.
                IdempotencyRecord winner = idempotencyRecordRepository
                        .findByIdempotencyKey(request.getIdempotencyKey())
                        .orElseThrow(() -> TransferServiceException.transferFailed(
                                "Idempotency key conflict but record not found after unique-key race"
                        ));
                log.info(
                        "Recovered from unique-key race via replay: idempotencyKey={}, transferId={}",
                        request.getIdempotencyKey(),
                        winner.getTransferId()
                );
                return handleExistingIdempotency(winner, requestHash);
            }

            Transfer transfer = created.get();
            log.info(
                    "Created PENDING transfer: transferId={}, idempotencyKey={}",
                    transfer.getId(),
                    transfer.getIdempotencyKey()
            );
            TransferResponse response = executeTransferSafely(transfer.getId());
            transferMetrics.markOutcome(TransferMetricOutcome.NEW_PROCESSED, response.getAmount());
            return response;
        } catch (Exception ex) {
            log.error(
                    "Transfer processing failed for idempotencyKey={}: {}",
                    request.getIdempotencyKey(),
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }

    private void ensureWalletsExist(String fromWalletId, String toWalletId) {
        log.debug("Checking wallets exist: fromWalletId={}, toWalletId={}", fromWalletId, toWalletId);
        boolean fromExists = walletRepository.existsById(fromWalletId);
        boolean toExists = walletRepository.existsById(toWalletId);
        if (!fromExists || !toExists) {
            log.error(
                    "Wallet(s) not found: fromWalletId={} (exists={}), toWalletId={} (exists={})",
                    fromWalletId,
                    fromExists,
                    toWalletId,
                    toExists
            );
            throw TransferServiceException.walletsNotFound();
        }
    }

    private TransferResponse handleExistingIdempotency(IdempotencyRecord record, String requestHash) {
        if (!record.getRequestHash().equals(requestHash)) {
            log.error(
                    "Idempotency conflict for key={}: storedHash={}, requestHash={}",
                    record.getIdempotencyKey(),
                    record.getRequestHash(),
                    requestHash
            );
            throw TransferServiceException.idempotencyConflict();
        }

        Transfer transfer = transferRepository.findById(record.getTransferId())
                .orElseThrow(() -> TransferServiceException.transferFailed(
                        "Transfer not found for idempotency key"
                ));

        log.info(
                "Replaying idempotent request: transferId={}, state={}",
                transfer.getId(),
                transfer.getState()
        );
        log.debug(
                "Existing transfer details: transferId={}, from={}, to={}, amount={}, state={}",
                transfer.getId(),
                transfer.getFromWallet(),
                transfer.getToWallet(),
                transfer.getAmount(),
                transfer.getState()
        );

        if (transfer.getState() == TransferState.PROCESSED) {
            log.info("Returning existing PROCESSED transfer: transferId={}", transfer.getId());
            transferMetrics.markOutcome(TransferMetricOutcome.REPLAY_PROCESSED);
            return TransferResponse.from(transfer);
        }

        if (transfer.getState() == TransferState.PENDING || transfer.getState() == TransferState.RETRY) {
            log.info("Resuming {} transfer: transferId={}", transfer.getState(), transfer.getId());
            TransferResponse response = executeTransferSafely(transfer.getId());
            transferMetrics.markOutcome(TransferMetricOutcome.RESUME_PROCESSED, response.getAmount());
            return response;
        }

        if (transfer.getState() == TransferState.FAILED) {
            log.info("Starting RETRY flow for FAILED transfer: transferId={}", transfer.getId());
            transferMetrics.markRetry();
            TransferResponse response = retryTransfer(transfer.getId());
            transferMetrics.markOutcome(TransferMetricOutcome.RETRY_PROCESSED, response.getAmount());
            return response;
        }

        return TransferResponse.from(transfer);
    }

    /**
     * Attempts to insert PENDING transfer + idempotency row.
     * Returns empty when a concurrent request already claimed the same idempotency key
     * (unique constraint race); caller must reload and replay.
     */
    private Optional<Transfer> createPendingTransferWithIdempotency(
            CreateTransferRequest request,
            String requestHash
    ) {
        try {
            Transfer transfer = transactionTemplate.execute(status -> {
                Transfer pending = Transfer.createPending(
                        request.getIdempotencyKey(),
                        request.getFromWalletId(),
                        request.getToWalletId(),
                        request.getAmount()
                );
                transferRepository.save(pending);
                idempotencyRecordRepository.save(
                        IdempotencyRecord.create(
                                request.getIdempotencyKey(),
                                pending.getId(),
                                requestHash
                        )
                );
                log.debug(
                        "Persisted PENDING transfer and idempotency record: transferId={}, idempotencyKey={}",
                        pending.getId(),
                        request.getIdempotencyKey()
                );
                return pending;
            });
            return Optional.ofNullable(transfer);
        } catch (DataIntegrityViolationException ex) {
            // Only recover from concurrent claims of the same idempotency key.
            // Other integrity errors (unexpected constraints, etc.) must surface as-is.
            if (!isIdempotencyUniqueViolation(ex)) {
                throw ex;
            }
            transferMetrics.markUniqueKeyRace();
            log.info(
                    "Unique-key race on idempotencyKey={}; will reload and replay",
                    request.getIdempotencyKey()
            );
            return Optional.empty();
        }
    }

    private static boolean isIdempotencyUniqueViolation(DataIntegrityViolationException ex) {
        Throwable cause = ex.getMostSpecificCause();
        String message = cause != null && cause.getMessage() != null
                ? cause.getMessage()
                : ex.getMessage();
        if (message == null) {
            return false;
        }
        return message.contains("transfers_idempotency_key")
                || message.contains("idempotency_records");
    }

    private TransferResponse retryTransfer(UUID transferId) {
        try {
            return transactionTemplate.execute(status -> {
                Transfer transfer = transferRepository.findByIdForUpdate(transferId)
                        .orElseThrow(() -> TransferServiceException.transferFailed("Transfer not found"));

                if (transfer.getState() == TransferState.PROCESSED) {
                    return TransferResponse.from(transfer);
                }

                if (transfer.getState() == TransferState.FAILED) {
                    // Mark transfer eligible for retry
                    transfer.markRetry();
                    transferRepository.save(transfer);
                    log.info("Marked transfer as RETRY: transferId={}", transferId);
                } else if (transfer.getState() != TransferState.PENDING
                        && transfer.getState() != TransferState.RETRY) {
                    throw TransferServiceException.transferFailed(
                            "Transfer is not executable in state " + transfer.getState()
                    );
                }

                return completeTransfer(transferId);
            });
        } catch (TransferServiceException ex) {
            log.error(
                    "Retry transfer failed with service exception: transferId={}, code={}, message={}",
                    transferId,
                    ex.getErrorCode(),
                    ex.getMessage()
            );
            // Persist FAILED only after locks are released, and only for executable-path failures.
            markFailedAfterExecutionException(transferId, ex);
            throw ex;
        } catch (RuntimeException ex) {
            log.error(
                    "Retry transfer failed unexpectedly: transferId={}: {}",
                    transferId,
                    ex.getMessage(),
                    ex
            );
            transferStatusService.markFailed(transferId, ex.getMessage());
            throw TransferServiceException.transferFailed(trim(ex.getMessage()));
        }
    }

    private TransferResponse executeTransferSafely(UUID transferId) {
        try {
            log.debug("Executing transfer transaction: transferId={}", transferId);
            return transactionTemplate.execute(status -> completeTransfer(transferId));
        } catch (TransferServiceException ex) {
            log.error(
                    "Transfer execution failed with service exception: transferId={}, code={}, message={}",
                    transferId,
                    ex.getErrorCode(),
                    ex.getMessage()
            );
            // Persist FAILED only after locks are released, and only for executable-path failures.
            markFailedAfterExecutionException(transferId, ex);
            throw ex;
        } catch (RuntimeException ex) {
            log.error(
                    "Transfer execution failed unexpectedly: transferId={}: {}",
                    transferId,
                    ex.getMessage(),
                    ex
            );
            transferStatusService.markFailed(transferId, ex.getMessage());
            throw TransferServiceException.transferFailed(trim(ex.getMessage()));
        }
    }

    /**
     * After the outer TX rolls back (releasing FOR UPDATE locks), persist FAILED only when the
     * failure happened while the transfer was executable (PENDING/RETRY). Other codes — e.g.
     * TRANSFER_FAILED for a non-executable state — must not overwrite transfer state.
     */
    private void markFailedAfterExecutionException(UUID transferId, TransferServiceException ex) {
        String errorCode = ex.getErrorCode();
        if ("INSUFFICIENT_BALANCE".equals(errorCode)) {
            transferStatusService.markFailed(transferId, INSUFFICIENT_BALANCE);
            return;
        }
        if ("WALLETS_NOT_FOUND".equals(errorCode)) {
            transferStatusService.markFailed(transferId, trim(ex.getMessage()));
            return;
        }
        log.debug(
                "Skipping markFailed for transferId={}, errorCode={} (not an executable-path failure)",
                transferId,
                errorCode
        );
    }

    private TransferResponse completeTransfer(UUID transferId) {
        Transfer transfer = transferRepository.findByIdForUpdate(transferId)
                .orElseThrow(() -> TransferServiceException.transferFailed("Transfer not found"));

        if (transfer.getState() == TransferState.PROCESSED) {
            log.info("Transfer already PROCESSED: transferId={}", transferId);
            return TransferResponse.from(transfer);
        }

        if (transfer.getState() != TransferState.PENDING && transfer.getState() != TransferState.RETRY) {
            log.error(
                    "Transfer not executable: transferId={}, state={}",
                    transferId,
                    transfer.getState()
            );
            throw TransferServiceException.transferFailed(
                    "Transfer is not executable in state " + transfer.getState()
            );
        }

        List<String> orderedWalletIds = List.of(transfer.getFromWallet(), transfer.getToWallet())
                .stream()
                .sorted(Comparator.naturalOrder())
                .toList();

        log.debug(
                "Acquiring wallet locks in order {} for transferId={}",
                orderedWalletIds,
                transferId
        );

        // Sort wallets by id and then take lock to avoid deadlocks
        Wallet first = walletRepository.findByIdForUpdate(orderedWalletIds.get(0))
                .orElseThrow(TransferServiceException::walletsNotFound);
        Wallet second = walletRepository.findByIdForUpdate(orderedWalletIds.get(1))
                .orElseThrow(TransferServiceException::walletsNotFound);

        Wallet fromWallet = first.getId().equals(transfer.getFromWallet()) ? first : second;
        Wallet toWallet = first.getId().equals(transfer.getToWallet()) ? first : second;

        if (!fromWallet.hasSufficientBalance(transfer.getAmount())) {
            log.error(
                    "Insufficient balance: transferId={}, fromWalletId={}, balance={}, amount={}",
                    transferId,
                    fromWallet.getId(),
                    fromWallet.getBalance(),
                    transfer.getAmount()
            );
            // Do not mark FAILED here: this TX still holds FOR UPDATE locks. Caller marks FAILED
            // after rollback so TransferStatusService (REQUIRES_NEW) can update the row.
            throw TransferServiceException.insufficientBalance(INSUFFICIENT_BALANCE);
        }

        fromWallet.debit(transfer.getAmount());
        toWallet.credit(transfer.getAmount());
        walletRepository.save(fromWallet);
        walletRepository.save(toWallet);

        ledgerEntryRepository.save(
                LedgerEntry.debit(fromWallet.getId(), transfer.getId(), transfer.getAmount())
        );
        ledgerEntryRepository.save(
                LedgerEntry.credit(toWallet.getId(), transfer.getId(), transfer.getAmount())
        );

        transfer.markProcessed();
        transferRepository.save(transfer);

        log.info(
                "Transfer completed successfully: transferId={}, fromWalletId={}, toWalletId={}, amount={}",
                transferId,
                fromWallet.getId(),
                toWallet.getId(),
                transfer.getAmount()
        );
        log.debug(
                "Post-transfer balances: fromWalletId={} balance={}, toWalletId={} balance={}",
                fromWallet.getId(),
                fromWallet.getBalance(),
                toWallet.getId(),
                toWallet.getBalance()
        );

        return TransferResponse.from(transfer);
    }

    private static String trim(String message) {
        if (message == null || message.isBlank()) {
            return "Unknown error";
        }
        return message.length() <= 500 ? message : message.substring(0, 500);
    }
}
