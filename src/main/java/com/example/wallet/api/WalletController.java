package com.example.wallet.api;

import com.example.wallet.service.WalletService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
import jakarta.validation.Valid;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@RestController
@RequestMapping("/wallet/v1")
public class WalletController {
    private final WalletService walletService;
    private static final Logger logger = LoggerFactory.getLogger(WalletController.class);

    public WalletController(WalletService service) {
        this.walletService = service;
    }

    @Operation(
            summary = "Create a wallet transfer",
            description = """
        Transfers money from one wallet to another.

        The operation is idempotent using the supplied idempotencyKey.
        Retrying the same request with the same idempotency key returns
        the original transfer result.

        The transfer is atomic:
        - Source wallet is debited.
        - Destination wallet is credited.
        - Debit and credit ledger entries are created.
        - If any operation fails, the entire transaction is rolled back.
        """
    )
    @ApiResponses({
            @ApiResponse(
                    responseCode = "200",
                    description = "Transfer completed successfully"
            ),
            @ApiResponse(
                    responseCode = "400",
                    description = "Invalid transfer request"
            ),
            @ApiResponse(
                    responseCode = "404",
                    description = "Source or destination wallet not found"
            ),
            @ApiResponse(
                    responseCode = "409",
                    description = "Idempotency conflict or insufficient funds"
            )
    })
    @PostMapping("/transfers")
    public ResponseEntity<TransferResponse> transfer(@Valid @RequestBody TransferRequest transferRequest) {
        logger.info("Processing transfer request: {}", transferRequest);
        return ResponseEntity.ok(walletService.transfer(transferRequest));
    }

    @GetMapping("/{walletId}/balance")
    public ResponseEntity<WalletBalanceResponse> getBalance(
            @PathVariable String walletId) {

        logger.info("Processing get balance request for wallet: {}", walletId);
        return ResponseEntity.ok(walletService.getBalance(walletId));
    }
}
