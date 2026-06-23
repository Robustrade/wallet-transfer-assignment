package com.example.wallet.service;

import com.example.wallet.domain.Transfer;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.web.dto.TransferRequest;
import com.example.wallet.web.dto.TransferResponse;
import java.util.Optional;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;

@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final IdempotencyService idempotencyService;
    private final TransferExecutor transferExecutor;

    public TransferService(
            TransferRepository transferRepository,
            IdempotencyService idempotencyService,
            TransferExecutor transferExecutor) {
        this.transferRepository = transferRepository;
        this.idempotencyService = idempotencyService;
        this.transferExecutor = transferExecutor;
    }

    public TransferExecutionResult createTransfer(TransferRequest request) {
        Optional<TransferExecutionResult> cached = idempotencyService.findCachedResult(request.idempotencyKey());
        if (cached.isPresent()) {
            return cached.get();
        }

        Optional<Transfer> existingTransfer =
                transferRepository.findByIdempotencyKey(request.idempotencyKey());
        if (existingTransfer.isPresent()) {
            return buildResultFromTransfer(existingTransfer.get());
        }

        try {
            return transferExecutor.execute(request);
        } catch (DataIntegrityViolationException exception) {
            return resolveDuplicateRequest(request.idempotencyKey());
        }
    }

    private TransferExecutionResult resolveDuplicateRequest(String idempotencyKey) {
        Optional<TransferExecutionResult> cached = idempotencyService.findCachedResult(idempotencyKey);
        if (cached.isPresent()) {
            return cached.get();
        }

        return transferRepository
                .findByIdempotencyKey(idempotencyKey)
                .map(this::buildResultFromTransfer)
                .orElseThrow(() -> new IllegalStateException(
                        "Duplicate idempotency key detected but no transfer record found: " + idempotencyKey));
    }

    private TransferExecutionResult buildResultFromTransfer(Transfer transfer) {
        int statusCode = switch (transfer.getStatus()) {
            case PROCESSED -> HttpStatus.CREATED.value();
            case FAILED -> HttpStatus.UNPROCESSABLE_ENTITY.value();
            case PENDING -> HttpStatus.CONFLICT.value();
        };
        return new TransferExecutionResult(statusCode, toResponse(transfer));
    }

    private TransferResponse toResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getErrorMessage());
    }
}
