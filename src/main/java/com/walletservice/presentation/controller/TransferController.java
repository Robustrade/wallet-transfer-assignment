package com.walletservice.presentation.controller;

import com.walletservice.application.service.TransferService;
import com.walletservice.domain.entity.Transfer;
import com.walletservice.presentation.dto.CreateTransferRequest;
import com.walletservice.presentation.dto.TransferResponse;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.slf4j.Logger;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.UUID;

/**
 * REST Controller for transfer operations.
 *
 * Handler responsibilities:
 * - Request validation (delegated to validation annotations)
 * - Transport mapping (DTO <-> domain model)
 * - HTTP status codes
 * - Invoking service logic
 *
 * Business logic is delegated to TransferService.
 */
@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
@Slf4j
public class TransferController {
    private final TransferService transferService;

    private static final Logger log
            = org.slf4j.LoggerFactory.getLogger(TransferController.class);
    /**
     * Creates a new wallet-to-wallet transfer.
     *
     * @param request transfer creation request including idempotencyKey
     * @return 201 Created with transfer details
     */
    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        log.info("Received transfer request: idempotencyKey={}", request.getIdempotencyKey());
//UUID.nameUUIDFromBytes(request.getFromWalletId().toString().getBytes());
        Transfer transfer = transferService.createTransfer(
            request.getFromWalletId(),
            request.getToWalletId(),
            request.getAmount(),
            request.getIdempotencyKey()
        );

        TransferResponse response = mapToResponse(transfer);
        return ResponseEntity.status(HttpStatus.CREATED).body(response);
    }

    /**
     * Retrieves a transfer by ID.
     *
     * @param transferId the transfer UUID
     * @return 200 OK with transfer details
     */
    @GetMapping("/{transferId}")
    public ResponseEntity<TransferResponse> getTransfer(@PathVariable String transferId) {
        log.info("Retrieving transfer: {}", transferId);

        Transfer transfer = transferService.getTransfer(transferId);
        TransferResponse response = mapToResponse(transfer);
        return ResponseEntity.ok(response);
    }

    /**
     * Maps a Transfer domain object to the HTTP response DTO.
     */
    private TransferResponse mapToResponse(Transfer transfer) {
        return TransferResponse.builder()
            .id(transfer.getId())
            .fromWalletId(transfer.getFromWalletId())
            .toWalletId(transfer.getToWalletId())
            .amount(transfer.getAmount())
            .status(transfer.getStatus())
            .createdAt(transfer.getCreatedAt())
            .updatedAt(transfer.getUpdatedAt())
            .build();
    }
}

