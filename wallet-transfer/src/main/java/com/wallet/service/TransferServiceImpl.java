package com.wallet.service;

import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.InsufficientFundsException;
import com.wallet.exception.SelfTransferException;
import com.wallet.exception.TransferNotFoundException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.TransferRepository;
import com.wallet.service.TransferOrchestrator.IdempotencyOutcome;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TransferServiceImpl implements TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferServiceImpl.class);

    private final TransferOrchestrator orchestrator;
    private final TransferRepository transferRepository;

    public TransferServiceImpl(TransferOrchestrator orchestrator, TransferRepository transferRepository) {
        this.orchestrator = orchestrator;
        this.transferRepository = transferRepository;
    }

    @Override
    public TransferExecutionResult executeTransfer(TransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new SelfTransferException(request.fromWalletId());
        }

        // Phase 1: commit PENDING record in its own transaction (via orchestrator proxy)
        IdempotencyOutcome outcome = orchestrator.getOrCreatePendingTransfer(request);

        if (outcome.isDuplicate()) {
            log.debug("Idempotent replay: transfer {} in state {}",
                outcome.transfer().getId(), outcome.transfer().getStatus());
            return TransferExecutionResult.duplicate(TransferResponse.from(outcome.transfer()));
        }

        try {
            // Phase 2: lock wallets, update balances, write ledger entries (via orchestrator proxy)
            TransferResponse response = orchestrator.processTransfer(outcome.transfer());
            return TransferExecutionResult.newTransfer(response);
        } catch (InsufficientFundsException | WalletNotFoundException ex) {
            // Phase 3: persist FAILED state in its own transaction (Phase 2 already rolled back)
            orchestrator.markTransferFailed(outcome.transfer().getId(), ex.getMessage());
            throw ex;
        } catch (RuntimeException ex) {
            // Phase 3: unexpected Phase 2 failure — mark FAILED so transfer is not stuck as PENDING
            orchestrator.markTransferFailed(outcome.transfer().getId(), ex.getMessage());
            throw ex;
        }
    }

    @Override
    @Transactional(readOnly = true)
    public TransferResponse getTransfer(UUID transferId) {
        return transferRepository.findById(transferId)
            .map(TransferResponse::from)
            .orElseThrow(() -> new TransferNotFoundException(transferId));
    }
}
