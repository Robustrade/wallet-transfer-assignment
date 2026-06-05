package com.example.wallet.service.impl;

import com.example.wallet.exception.InvalidTransferStateException;
import com.example.wallet.model.Transfer;
import com.example.wallet.model.TransferStatus;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.service.TransferMetrics;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionTemplate;

import java.time.Duration;
import java.time.Instant;
import java.util.List;

/**
 * Scheduled safety-net that resolves transfers stuck in PENDING state.
 *
 * Why PENDING transfers can get stuck:
 *   - Application process killed (SIGKILL) while a transaction was open
 *     → PostgreSQL rolls back the open transaction, but if the process restarts
 *       before the rollback propagates, the row may briefly appear as PENDING.
 *   - Future async/queue-based processing where PENDING means "enqueued"
 *     and a worker crash could leave it unresolved.
 *   - Any uncaught exception that bypassed the normal FAILED transition path.
 *
 * Strategy: mark stuck PENDING transfers as FAILED after a configurable timeout.
 * The updateStatus guard (WHERE status = 'PENDING') ensures this is safe under
 * concurrent execution — if a real transaction completes the transfer between
 * find and update, the update is a no-op and InvalidTransferStateException is
 * caught and silently ignored.
 */
@Slf4j
@Service
@RequiredArgsConstructor
public class PendingTransferCleanupService {

    private final TransferRepository transferRepository;
    private final TransactionTemplate transactionTemplate;
    private final TransferMetrics metrics;

    @Value("${wallet.pending-transfer.stuck-after-minutes:5}")
    private int stuckAfterMinutes;

    /**
     * Runs every minute by default (configurable via wallet.pending-transfer.cleanup-interval-ms).
     * Finds transfers stuck in PENDING longer than stuckAfterMinutes and marks them FAILED.
     */
    @Scheduled(fixedDelayString = "${wallet.pending-transfer.cleanup-interval-ms:60000}")
    public void resolveStuckTransfers() {
        Instant stuckBefore = Instant.now().minus(Duration.ofMinutes(stuckAfterMinutes));
        List<Transfer> stuck = transferRepository.findStuckPending(stuckBefore);

        if (stuck.isEmpty()) {
            return;
        }

        log.warn("Found {} stuck PENDING transfer(s) older than {} minutes — marking as FAILED",
                stuck.size(), stuckAfterMinutes);

        for (Transfer transfer : stuck) {
            resolveOne(transfer);
        }
    }

    private void resolveOne(Transfer transfer) {
        try {
            transactionTemplate.execute(status -> {
                // updateStatus has WHERE status = 'PENDING' guard — safe under concurrent execution.
                // If the transfer was legitimately completed between findStuckPending and here,
                // updateStatus throws InvalidTransferStateException (updated = 0). We catch it below.
                transferRepository.updateStatus(transfer.id(), TransferStatus.FAILED);
                return null;
            });
            log.warn("Resolved stuck transfer transferId={} createdAt={} fromWallet={} amount={}",
                    transfer.id(), transfer.createdAt(), transfer.fromWalletId(), transfer.amount());
            metrics.recordCleanupResolved();
        } catch (InvalidTransferStateException e) {
            // Expected: transfer was completed by a real transaction between find and update.
            log.debug("Transfer {} already completed before cleanup ran — skipping", transfer.id());
        } catch (Exception e) {
            // Non-fatal: log and continue to next transfer.
            log.error("Failed to resolve stuck transfer transferId={}", transfer.id(), e);
        }
    }
}

