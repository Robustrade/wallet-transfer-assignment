package com.assignment.wallet_transfer.controller;

import com.assignment.wallet_transfer.dtos.TransferRequest;
import com.assignment.wallet_transfer.dtos.TransferResponse;
import com.assignment.wallet_transfer.service.TransferService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/transfers")
@Tag(name = "Wallet APIs", description = "Wallet transfer operations")
@RequiredArgsConstructor
public class TransferController {

    private final TransferService transferService;

    @PostMapping
    @Operation(summary = "Transfer money between wallets")
    public ResponseEntity<TransferResponse> transfer(
            @RequestBody @Valid TransferRequest request,
            @RequestHeader("Idempotency-Key")
            String idempotencyKey
    ) {

        return ResponseEntity.ok(
                transferService.transfer(
                        request,
                        idempotencyKey
                )
        );
    }

}
