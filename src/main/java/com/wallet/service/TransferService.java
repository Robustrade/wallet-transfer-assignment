package com.wallet.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.api.dto.CreateTransferRequest;
import com.wallet.api.dto.TransferDetailResponse;
import com.wallet.api.dto.TransferResponse;
import com.wallet.api.exception.IdempotencyConflictException;
import com.wallet.api.exception.InvalidTransferException;
import com.wallet.api.exception.TransferNotFoundException;
import com.wallet.api.exception.WalletNotFoundException;
import com.wallet.domain.LedgerEntryType;
import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import com.wallet.repository.IdempotencyRepository;
import com.wallet.repository.LedgerRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import java.time.Instant;
import java.util.UUID;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerRepository ledgerRepository;
    private final IdempotencyRepository idempotencyRepository;
    private final ObjectMapper objectMapper;

    public TransferService(
            TransferRepository transferRepository,
            WalletRepository walletRepository,
            LedgerRepository ledgerRepository,
            IdempotencyRepository idempotencyRepository,
            ObjectMapper objectMapper) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
        this.ledgerRepository = ledgerRepository;
        this.idempotencyRepository = idempotencyRepository;
        this.objectMapper = objectMapper;
    }

    @Transactional
    public TransferExecutionResult createTransfer(CreateTransferRequest request) {
        validateRequest(request);
        String requestHash = RequestHashUtil.hash(request);

        var cached = idempotencyRepository.findByKey(request.idempotencyKey());
        if (cached.isPresent()) {
            return replayCachedResponse(cached.get(), requestHash, request.idempotencyKey());
        }

        UUID transferId = UUID.randomUUID();
        Instant now = Instant.now();
        Transfer pendingTransfer =
                new Transfer(
                        transferId,
                        request.idempotencyKey(),
                        request.fromWalletId(),
                        request.toWalletId(),
                        request.amount(),
                        TransferStatus.PENDING,
                        null,
                        now,
                        now);

        try {
            transferRepository.insert(pendingTransfer);
        } catch (DuplicateKeyException ex) {
            return handleDuplicateTransfer(request, requestHash);
        }

        if (!idempotencyRepository.tryInsert(
                request.idempotencyKey(), transferId, requestHash)) {
            return handleDuplicateTransfer(request, requestHash);
        }

        var fromWallet =
                walletRepository
                        .findByIdForUpdate(request.fromWalletId())
                        .orElseThrow(() -> new WalletNotFoundException(request.fromWalletId()));
        walletRepository
                .findByIdForUpdate(request.toWalletId())
                .orElseThrow(() -> new WalletNotFoundException(request.toWalletId()));

        TransferResponse response;
        int httpStatus;
        if (fromWallet.balance() < request.amount()) {
            transferRepository.updateStatus(
                    transferId, TransferStatus.FAILED, "Insufficient funds");
            response =
                    toResponse(
                            pendingTransfer.withStatus(TransferStatus.FAILED, "Insufficient funds"));
            httpStatus = 422;
        } else {
            walletRepository.debit(request.fromWalletId(), request.amount());
            walletRepository.credit(request.toWalletId(), request.amount());
            ledgerRepository.insert(
                    request.fromWalletId(), transferId, LedgerEntryType.DEBIT, request.amount());
            ledgerRepository.insert(
                    request.toWalletId(), transferId, LedgerEntryType.CREDIT, request.amount());
            transferRepository.updateStatus(transferId, TransferStatus.PROCESSED, null);
            response =
                    toResponse(pendingTransfer.withStatus(TransferStatus.PROCESSED, null));
            httpStatus = 201;
        }

        persistIdempotencyResponse(request.idempotencyKey(), response, httpStatus);
        return new TransferExecutionResult(response, httpStatus);
    }

    @Transactional(readOnly = true)
    public TransferDetailResponse getTransfer(UUID transferId) {
        Transfer transfer =
                transferRepository
                        .findById(transferId)
                        .orElseThrow(() -> new TransferNotFoundException(transferId.toString()));

        var ledgerEntries =
                ledgerRepository.findByTransferId(transferId).stream()
                        .map(
                                entry ->
                                        new TransferDetailResponse.LedgerEntryResponse(
                                                entry.id(),
                                                entry.walletId(),
                                                entry.entryType(),
                                                entry.amount()))
                        .toList();

        return new TransferDetailResponse(
                transfer.id().toString(),
                transfer.status(),
                transfer.fromWalletId(),
                transfer.toWalletId(),
                transfer.amount(),
                transfer.failureReason(),
                ledgerEntries);
    }

    private TransferExecutionResult handleDuplicateTransfer(
            CreateTransferRequest request, String requestHash) {
        var existingRecord = idempotencyRepository.findByKey(request.idempotencyKey());
        if (existingRecord.isPresent()) {
            return replayCachedResponse(
                    existingRecord.get(), requestHash, request.idempotencyKey());
        }

        var existingTransfer =
                transferRepository
                        .findByIdempotencyKey(request.idempotencyKey())
                        .orElseThrow(
                                () ->
                                        new IllegalStateException(
                                                "Duplicate idempotency key without transfer record"));

        TransferResponse response = toResponse(existingTransfer);
        int httpStatus = existingTransfer.status() == TransferStatus.PROCESSED ? 201 : 422;
        persistIdempotencyResponse(request.idempotencyKey(), response, httpStatus);
        return new TransferExecutionResult(response, httpStatus);
    }

    private TransferExecutionResult replayCachedResponse(
            com.wallet.domain.IdempotencyRecord record,
            String requestHash,
            String idempotencyKey) {
        if (!record.requestHash().equals(requestHash)) {
            throw new IdempotencyConflictException(idempotencyKey);
        }
        if (record.httpStatus() <= 0) {
            throw new IdempotencyConflictException(idempotencyKey);
        }
        try {
            TransferResponse response =
                    objectMapper.readValue(record.responseBody(), TransferResponse.class);
            return new TransferExecutionResult(response, record.httpStatus());
        } catch (JsonProcessingException ex) {
            throw new IllegalStateException("Failed to deserialize cached transfer response", ex);
        }
    }

    private void persistIdempotencyResponse(
            String idempotencyKey, TransferResponse response, int httpStatus) {
        try {
            idempotencyRepository.updateResponse(
                    idempotencyKey, objectMapper.writeValueAsString(response), httpStatus);
        } catch (JsonProcessingException ex) {
            throw new IllegalStateException("Failed to serialize transfer response", ex);
        }
    }

    private void validateRequest(CreateTransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidTransferException("Source and destination wallets must differ");
        }
    }

    private TransferResponse toResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.id().toString(),
                transfer.status(),
                transfer.fromWalletId(),
                transfer.toWalletId(),
                transfer.amount(),
                transfer.failureReason());
    }
}
