package com.wallet.transfer.handler;

import com.wallet.transfer.model.TransferRequest;
import com.wallet.transfer.model.TransferResult;
import com.wallet.transfer.service.TransferService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.Optional;

/**
 * REST controller for managing wallet-to-wallet transfer transactions.
 * Provides endpoints for executing and verifying money transfers.
 */
@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;

    /**
     * Constructs a new TransferController with the required TransferService dependency.
     *
     * @param transferService The service containing wallet-to-wallet transfer business logic.
     */
    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    /**
     * Handles wallet-to-wallet transfer requests.
     * This endpoint implements a high-performance, double-checked idempotency check.
     * Before starting a database transaction or acquiring locks, it first performs a fast-path 
     * check in the cached/recorded response store. If a request with the same idempotency key 
     * is found, it immediately returns the original cached response (satisfying idempotency).
     * Otherwise, it proceeds to execute the transfer logic within the transactional service layer.
     *
     * @param request Validated transfer request payload.
     * @return ResponseEntity containing JSON response body and appropriate HTTP status.
     */
    @PostMapping
    public ResponseEntity<String> createTransfer(@Valid @RequestBody TransferRequest request) {
        // Fast-path: Check cached/recorded result outside the database transaction boundary
        // to avoid unnecessary transaction overhead or database connection hold time.
        Optional<TransferResult> cached = transferService.getCachedResult(request.getIdempotencyKey());
        if (cached.isPresent()) {
            TransferResult result = cached.get();
            return ResponseEntity.status(result.getStatusCode())
                    .header("Content-Type", "application/json")
                    .body(result.getResponseBody());
        }

        // Execute transfer inside the transaction boundary and write lock context
        TransferResult result = transferService.executeTransfer(request);
        return ResponseEntity.status(result.getStatusCode())
                .header("Content-Type", "application/json")
                .body(result.getResponseBody());
    }
}
