package com.wallet.transferservice.controller;

import com.wallet.transferservice.dto.TransferRequest;
import com.wallet.transferservice.service.TransferService;
import com.wallet.transferservice.service.TransferService.TransferResult;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    /**
     * POST /transfers
     *
     * Creates a new wallet-to-wallet transfer.
     * Returns 201 Created for new transfers, 200 OK for idempotent replays.
     */
    @PostMapping
    public ResponseEntity<?> createTransfer(@Valid @RequestBody TransferRequest request) {
        TransferResult result = transferService.executeTransfer(request);

        HttpStatus status = result.replay() ? HttpStatus.OK : HttpStatus.CREATED;
        return ResponseEntity.status(status).body(result.response());
    }
}
