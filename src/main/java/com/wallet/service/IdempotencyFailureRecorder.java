package com.wallet.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.domain.Transfer;
import com.wallet.metrics.TransferMetrics;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.util.LogSafe;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionSynchronization;
import org.springframework.transaction.support.TransactionSynchronizationManager;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;

/**
 * Persists FAILED state in a fresh transaction ({@link Propagation#REQUIRES_NEW})
 * after the executing transaction has rolled back, writing a FAILED Transfer row
 * (when wallets exist) and a FAILED idempotency record so replays return the
 * cached error. INSERT-only, never overwrites — a concurrent retry's outcome wins.
 */
@Service
public class IdempotencyFailureRecorder {

    private static final Logger log = LoggerFactory.getLogger(IdempotencyFailureRecorder.class);

    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final TransferRepository transferRepository;
    private final ObjectMapper objectMapper;
    private final TransferMetrics metrics;

    public IdempotencyFailureRecorder(IdempotencyRecordRepository idempotencyRecordRepository,
                                      TransferRepository transferRepository,
                                      ObjectMapper objectMapper,
                                      TransferMetrics metrics) {
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.transferRepository = transferRepository;
        this.objectMapper = objectMapper;
        this.metrics = metrics;
    }

    /** No Transfer row is written — the wallet FK cannot be satisfied. */
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void recordWalletNotFoundFailure(String clientId, String idempotencyKey, String requestHash,
                                            String message) {
        String errorJson = formatErrorJson(HttpStatus.NOT_FOUND, message);
        insertFailureOrSkip(clientId, idempotencyKey, requestHash, null,
                HttpStatus.NOT_FOUND.value(), errorJson);
        recordFailureMetricAfterCommit(TransferMetrics.REASON_WALLET_NOT_FOUND);
    }

    /**
     * Writes the audit Transfer row first, then claims the idempotency record
     * with {@code transfer_id} pre-populated in a single statement — mirrors
     * the happy-path ordering in
     * {@link TransferExecutionService#executeBalancedTransfer}. The Transfer
     * table has no FK back to {@code idempotency_records}, so an
     * insert-then-skipped-claim leaves at most an unreferenced FAILED audit
     * row, never the inverse: a cached 422 with no audit row. Trade-off
     * accepted because losing audit data is worse than carrying an
     * occasional orphan that the existing FAILED claim's response will
     * supersede in replays.
     *
     * <p><strong>OPS contract:</strong> reconciliation tooling that joins
     * {@code transfers} to {@code idempotency_records.transfer_id} MUST
     * tolerate orphaned FAILED transfers (LEFT JOIN, not INNER JOIN). The
     * orphans are bounded — one per lost-claim race per failure — and the
     * existing FAILED claim's response supersedes them on replay.
     *
     * <p><strong>Sweep status:</strong> there is no sweep job today.
     * {@link IdempotencyCleanupService} only evicts {@code idempotency_records},
     * not orphan {@code transfers}. The orphan rate is genuinely small (one
     * row per lost claim race, per failure), so the bound holds without one;
     * if/when the operational shape demands it, a periodic sweep that flags
     * or archives FAILED transfers with no inbound idempotency reference
     * older than the replay TTL would close the loop. Until then, this is an
     * aspirational hook, not a contract — tracked as a backlog item, not a
     * silent dependency.
     */
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void recordInsufficientBalanceFailure(String clientId, String idempotencyKey, String requestHash,
                                                 CreateTransferRequest request, long availableBalance) {
        String errorMsg = "Insufficient balance in wallet " + request.fromWalletId()
                + ": available=" + availableBalance + ", requested=" + request.amount();
        String errorJson = formatErrorJson(HttpStatus.UNPROCESSABLE_ENTITY, errorMsg);

        Transfer transfer = Transfer.failed(
                request.fromWalletId(), request.toWalletId(), request.amount(), errorMsg);
        transferRepository.insert(transfer);

        int claimed = idempotencyRecordRepository.insertFailedRecord(
                clientId, idempotencyKey, requestHash, transfer.getId(),
                HttpStatus.UNPROCESSABLE_ENTITY.value(), errorJson);
        if (claimed == 0) {
            log.info("Idempotency claim lost to concurrent retry — keeping audit Transfer row: client={}, key={}, transferId={}",
                    clientId, LogSafe.mask(idempotencyKey), transfer.getId());
        }
        recordFailureMetricAfterCommit(TransferMetrics.REASON_INSUFFICIENT_BALANCE);
    }

    /**
     * Registers the per-reason failure counter on the {@code afterCommit} hook
     * of the active REQUIRES_NEW transaction so it only fires once the FAILED
     * row is durably persisted. If we incremented immediately, a post-INSERT
     * commit failure would leave the counter ahead of reality.
     */
    private void recordFailureMetricAfterCommit(String reason) {
        if (TransactionSynchronizationManager.isSynchronizationActive()) {
            TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
                @Override
                public void afterCommit() {
                    metrics.recordFailure(reason);
                }
            });
        } else {
            metrics.recordFailure(reason);
        }
    }

    /** Inserts a FAILED record; NO-OP if one already exists (a concurrent retry won the claim). */
    private void insertFailureOrSkip(String clientId, String idempotencyKey, String requestHash,
                                     UUID transferId, int responseStatus, String responseBody) {
        int rowsInserted = idempotencyRecordRepository.insertFailedRecord(
                clientId, idempotencyKey, requestHash, transferId, responseStatus, responseBody);
        if (rowsInserted == 0) {
            log.info("Skipping FAILED record write — concurrent record already present: client={}, key={}",
                    clientId, LogSafe.mask(idempotencyKey));
        }
    }

    /**
     * Captures the failure time once and embeds it in the cached body. Replays
     * return this capture-time, not the replay-time — semantics chosen for
     * audit fidelity (the response always reflects when the failure occurred).
     */
    private String formatErrorJson(HttpStatus status, String message) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("timestamp", Instant.now().toString());
        body.put("statusCode", status.value());
        body.put("error", status.getReasonPhrase());
        body.put("message", message);
        try {
            return objectMapper.writeValueAsString(body);
        } catch (Exception e) {
            throw new RuntimeException("Failed to serialize error response", e);
        }
    }
}
