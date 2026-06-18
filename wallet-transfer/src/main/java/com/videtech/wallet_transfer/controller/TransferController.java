package com.videtech.wallet_transfer.controller;

import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import com.videtech.wallet_transfer.domain.Transfer;
import com.videtech.wallet_transfer.dto.TransferRequest;
import com.videtech.wallet_transfer.dto.TransferResponse;
import com.videtech.wallet_transfer.service.TransferService;

import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {

    private final TransferService transferService;

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody TransferRequest request) {
        Transfer transfer = transferService.execute(
                request.getIdempotencyKey(),
                request.getFromWalletId(),
                request.getToWalletId(),
                request.getAmount()
        );
        if (transfer.getStatus() == com.videtech.wallet_transfer.domain.TransferStatus.FAILED) {
            return ResponseEntity.badRequest().body(toResponse(transfer));
        }
        return ResponseEntity.ok(toResponse(transfer));
    }

    private TransferResponse toResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getStatus().name(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getCreatedAt()
        );
    }
}
