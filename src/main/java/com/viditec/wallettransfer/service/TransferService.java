package com.viditec.wallettransfer.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.viditec.wallettransfer.api.response.ErrorResponse;
import com.viditec.wallettransfer.domain.EntryType;
import com.viditec.wallettransfer.domain.TransferState;
import com.viditec.wallettransfer.dto.CreateTransferDto;
import com.viditec.wallettransfer.dto.TransferDto;
import com.viditec.wallettransfer.exception.DuplicateIdempotencyKeyException;
import com.viditec.wallettransfer.exception.InsufficientFundsException;
import com.viditec.wallettransfer.exception.InvalidTransferRequestException;
import com.viditec.wallettransfer.exception.ResourceNotFoundException;
import com.viditec.wallettransfer.exception.TransferException;
import com.viditec.wallettransfer.repository.IdempotencyRecord;
import com.viditec.wallettransfer.repository.IdempotencyOperations;
import com.viditec.wallettransfer.repository.LedgerOperations;
import com.viditec.wallettransfer.repository.TransferOperations;
import com.viditec.wallettransfer.repository.WalletOperations;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionOperations;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;

@Service
public class TransferService {

    private final WalletOperations walletRepository;
    private final TransferOperations transferRepository;
    private final LedgerOperations ledgerRepository;
    private final IdempotencyOperations idempotencyRepository;
    private final TransactionOperations transactionTemplate;
    private final ObjectMapper objectMapper;

    public TransferService(
            WalletOperations walletRepository,
            TransferOperations transferRepository,
            LedgerOperations ledgerRepository,
            IdempotencyOperations idempotencyRepository,
            TransactionOperations transactionTemplate,
            ObjectMapper objectMapper
    ) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerRepository = ledgerRepository;
        this.idempotencyRepository = idempotencyRepository;
        this.transactionTemplate = transactionTemplate;
        this.objectMapper = objectMapper;
    }

    public ResponseEntity<?> createTransfer(CreateTransferDto request) {
        validateRequest(request);
        String requestHash = hashRequest(request);

        return transactionTemplate.execute(status -> {
            IdempotencyRecord existingRecord = idempotencyRepository.findByKey(request.idempotencyKey()).orElse(null);
            if (existingRecord != null) {
                if (!existingRecord.requestHash().equals(requestHash)) {
                    throw new DuplicateIdempotencyKeyException();
                }
                return deserializeStoredResponse(existingRecord);
            }

            boolean reserved = idempotencyRepository.reserve(request.idempotencyKey(), requestHash);
            if (!reserved) {
                IdempotencyRecord duplicate = idempotencyRepository.findByKey(request.idempotencyKey())
                        .orElseThrow(DuplicateIdempotencyKeyException::new);
                if (!duplicate.requestHash().equals(requestHash)) {
                    throw new DuplicateIdempotencyKeyException();
                }
                return deserializeStoredResponse(duplicate);
            }

            String transferId = transferRepository.createPending(
                    request.idempotencyKey(),
                    request.fromWalletId(),
                    request.toWalletId(),
                    request.amount()
            );

            try {
                ensureWalletExists(request.fromWalletId());
                ensureWalletExists(request.toWalletId());

                int debitedRows = walletRepository.debitIfSufficientFunds(request.fromWalletId(), request.amount());
                if (debitedRows == 0) {
                    throw new InsufficientFundsException(request.fromWalletId());
                }

                int creditedRows = walletRepository.credit(request.toWalletId(), request.amount());
                if (creditedRows == 0) {
                    throw new ResourceNotFoundException("Wallet", request.toWalletId());
                }

                ledgerRepository.createEntry(transferId, request.fromWalletId(), EntryType.DEBIT, request.amount());
                ledgerRepository.createEntry(transferId, request.toWalletId(), EntryType.CREDIT, request.amount());
                transferRepository.markProcessed(transferId);

                TransferDto response = new TransferDto(
                        transferId,
                        request.idempotencyKey(),
                        request.fromWalletId(),
                        request.toWalletId(),
                        request.amount(),
                        TransferState.PROCESSED
                );

                ResponseEntity<TransferDto> entity = ResponseEntity.status(HttpStatus.CREATED).body(response);
                persistIdempotentResult(request.idempotencyKey(), transferId, entity);
                return entity;
            } catch (TransferException exception) {
                transferRepository.markFailed(transferId, exception.getMessage());
                ResponseEntity<ErrorResponse> entity = mapBusinessFailure(exception);
                persistIdempotentResult(request.idempotencyKey(), transferId, entity);
                return entity;
            }
        });
    }

    private void ensureWalletExists(String walletId) {
        if (!walletRepository.exists(walletId)) {
            throw new ResourceNotFoundException("Wallet", walletId);
        }
    }

    private void validateRequest(CreateTransferDto request) {
        if (request.amount() <= 0) {
            throw new InvalidTransferRequestException(
                    "Transfer amount must be greater than zero."
            );
        }
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidTransferRequestException("Source and destination wallets must be different.");
        }
    }

    private ResponseEntity<?> deserializeStoredResponse(IdempotencyRecord record) {
        if (record.httpStatus() == null || record.responseBody() == null) {
            throw new IllegalStateException("Idempotency record is incomplete.");
        }
        try {
            HttpStatus httpStatus = HttpStatus.valueOf(record.httpStatus());
            if (httpStatus.is2xxSuccessful()) {
                TransferDto response = objectMapper.readValue(record.responseBody(), TransferDto.class);
                return ResponseEntity.status(httpStatus).body(response);
            }
            ErrorResponse errorResponse = objectMapper.readValue(record.responseBody(), ErrorResponse.class);
            return ResponseEntity.status(httpStatus).body(errorResponse);
        } catch (JsonProcessingException exception) {
            throw new IllegalStateException("Stored idempotency payload could not be deserialized.", exception);
        }
    }

    private ResponseEntity<ErrorResponse> mapBusinessFailure(TransferException exception) {
        if (exception instanceof ResourceNotFoundException) {
            return ResponseEntity.status(HttpStatus.NOT_FOUND)
                    .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
        }
        if (exception instanceof InsufficientFundsException) {
            return ResponseEntity.status(HttpStatus.UNPROCESSABLE_ENTITY)
                    .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
        }
        if (exception instanceof InvalidTransferRequestException) {
            return ResponseEntity.badRequest()
                    .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
        }
        return ResponseEntity.internalServerError()
                .body(new ErrorResponse("INTERNAL_ERROR", "An unexpected error occurred."));
    }

    private void persistIdempotentResult(String idempotencyKey, String transferId, ResponseEntity<?> responseEntity) {
        try {
            String responseBody = objectMapper.writeValueAsString(responseEntity.getBody());
            idempotencyRepository.complete(
                    idempotencyKey,
                    transferId,
                    responseEntity.getStatusCode().value(),
                    responseBody
            );
        } catch (JsonProcessingException exception) {
            throw new IllegalStateException("Failed to serialize transfer response.", exception);
        }
    }

    private String hashRequest(CreateTransferDto request) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            String raw = "%s|%s|%s|%d".formatted(
                    request.idempotencyKey(),
                    request.fromWalletId(),
                    request.toWalletId(),
                    request.amount()
            );
            return HexFormat.of().formatHex(digest.digest(raw.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is unavailable.", exception);
        }
    }
}
