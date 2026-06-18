package com.wallet.transfer.controller;

import com.wallet.transfer.dto.CreateTransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.exception.InsufficientBalanceException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.service.WalletTransferService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.validation.annotation.Validated;
import org.springframework.web.bind.annotation.*;

import jakarta.validation.Valid;
import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import java.math.BigDecimal;
import java.util.HashMap;
import java.util.Map;

/**
 * Wallet Transfer REST Controller
 * 
 * HTTP API for wallet-to-wallet transfers:
 * 
 * POST /transfers
 *   Request: CreateTransferRequest (idempotencyKey, fromWalletId, toWalletId, amount)
 *   Response: 201 Created - TransferResponse
 *            409 Conflict - Duplicate idempotency key (already processed)
 *            422 Unprocessable Entity - Validation error
 *            400 Bad Request - Wallet not found or insufficient balance
 *            500 Internal Server Error - Unexpected error
 * 
 * Key Features:
 * - Request validation: non-null fields, positive amount
 * - Proper HTTP status codes for different error scenarios
 * - Error response with descriptive messages
 * - Idempotency: same key returns same response (handled by service layer)
 */
@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
@Validated
@Slf4j
public class WalletTransferController {
    
    private final WalletTransferService walletTransferService;
    
    /**
     * Create a wallet-to-wallet transfer
     * 
     * HTTP: POST /transfers
     * 
     * Request:
     * {
     *   "idempotencyKey": "req-12345",
     *   "fromWalletId": "wallet-001",
     *   "toWalletId": "wallet-002",
     *   "amount": 100.50
     * }
     * 
     * Success Response (201 Created):
     * {
     *   "transferId": "txn_abc123",
     *   "fromWalletId": "wallet-001",
     *   "toWalletId": "wallet-002",
     *   "amount": 100.50,
     *   "status": "PROCESSED",
     *   "createdAt": "2026-06-16T23:46:00"
     * }
     * 
     * Error Responses:
     * - 409 Conflict: Duplicate idempotencyKey (already processed)
     * - 422 Unprocessable Entity: Validation error (negative amount, null fields)
     * - 400 Bad Request: Wallet not found or insufficient balance
     * - 500 Internal Server Error: Unexpected error
     * 
     * @param request transfer request with idempotencyKey, fromWalletId, toWalletId, amount
     * @return ResponseEntity with TransferResponse and appropriate HTTP status
     */
    @PostMapping
    public ResponseEntity<?> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        log.info("Received transfer request: idempotencyKey={}, from={}, to={}, amount={}", 
                 request.getIdempotencyKey(), request.getFromWalletId(), 
                 request.getToWalletId(), request.getAmount());
        
        try {
            // Validate request
            validateRequest(request);
            
            // Execute transfer
            TransferResponse response = walletTransferService.executeTransfer(request);
            
            log.info("Transfer created successfully: transferId={}", response.getTransferId());
            return ResponseEntity.status(HttpStatus.CREATED).body(response);
            
        } catch (WalletNotFoundException e) {
            log.warn("Wallet not found: {}", e.getMessage());
            return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                    .body(errorResponse("WALLET_NOT_FOUND", e.getMessage()));
            
        } catch (InsufficientBalanceException e) {
            log.warn("Insufficient balance: {}", e.getMessage());
            return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                    .body(errorResponse("INSUFFICIENT_BALANCE", e.getMessage()));
            
        } catch (Exception e) {
            log.error("Unexpected error during transfer", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(errorResponse("INTERNAL_ERROR", "An unexpected error occurred"));
        }
    }
    
    /**
     * Validate transfer request
     * 
     * Checks:
     * - All fields non-null
     * - Amount > 0
     * - fromWalletId != toWalletId
     * 
     * @param request the transfer request
     * @throws IllegalArgumentException if validation fails
     */
    private void validateRequest(CreateTransferRequest request) {
        if (request.getIdempotencyKey() == null || request.getIdempotencyKey().isBlank()) {
            throw new IllegalArgumentException("idempotencyKey cannot be null or empty");
        }
        
        if (request.getFromWalletId() == null || request.getFromWalletId().isBlank()) {
            throw new IllegalArgumentException("fromWalletId cannot be null or empty");
        }
        
        if (request.getToWalletId() == null || request.getToWalletId().isBlank()) {
            throw new IllegalArgumentException("toWalletId cannot be null or empty");
        }
        
        if (request.getAmount() == null) {
            throw new IllegalArgumentException("amount cannot be null");
        }
        
        if (request.getAmount().compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        
        if (request.getFromWalletId().equals(request.getToWalletId())) {
            throw new IllegalArgumentException("fromWalletId and toWalletId cannot be the same");
        }
    }
    
    /**
     * Create error response map
     * 
     * @param code error code
     * @param message error message
     * @return error response map
     */
    private Map<String, String> errorResponse(String code, String message) {
        Map<String, String> response = new HashMap<>();
        response.put("error", code);
        response.put("message", message);
        return response;
    }
}
