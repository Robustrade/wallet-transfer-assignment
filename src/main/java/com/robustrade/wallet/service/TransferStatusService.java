package com.robustrade.wallet.service;

import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.repository.TransferRepository;
import java.util.UUID;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

@Slf4j
@Service
@RequiredArgsConstructor
public class TransferStatusService {

    private final TransferRepository transferRepository;

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void markFailed(UUID transferId, String reason) {
        log.info("Marking transfer as FAILED: transferId={}, reason={}", transferId, reason);
        log.debug("markFailed details: transferId={}, reason={}", transferId, reason);

        try {
            // Lock the row so we cannot overwrite a concurrently committed PROCESSED transfer.
            Transfer transfer = transferRepository.findByIdForUpdate(transferId)
                    .orElseThrow(() -> new IllegalStateException("Transfer not found: " + transferId));
            if (transfer.getState() == TransferState.PROCESSED) {
                log.debug("Skipping markFailed; transfer already PROCESSED: transferId={}", transferId);
                return;
            }
            if (transfer.getState() == TransferState.FAILED
                    && reason != null
                    && reason.equals(transfer.getFailureReason())) {
                log.debug("Skipping markFailed; failure reason unchanged: transferId={}", transferId);
                return;
            }
            if (transfer.getState() == TransferState.PENDING || transfer.getState() == TransferState.RETRY) {
                transfer.markFailed(trimReason(reason));
                transferRepository.save(transfer);
                log.info("Transfer marked FAILED: transferId={}", transferId);
                return;
            }
            if (transfer.getState() == TransferState.FAILED) {
                transfer.setFailureReason(trimReason(reason));
                transfer.setUpdatedAt(java.time.Instant.now());
                transferRepository.save(transfer);
                log.info("Updated FAILED transfer reason: transferId={}", transferId);
            }
        } catch (Exception ex) {
            log.error(
                    "Failed to mark transfer as FAILED: transferId={}: {}",
                    transferId,
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }

    private static String trimReason(String reason) {
        if (reason == null || reason.isBlank()) {
            return "Unknown error";
        }
        return reason.length() <= 500 ? reason : reason.substring(0, 500);
    }
}
