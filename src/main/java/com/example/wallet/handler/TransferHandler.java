package com.example.wallet.handler;

import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.TransferResponse;
import com.example.wallet.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferHandler {

    private final TransferService transferService;

    /**
     * POST /transfers
     *
     * Creates a new wallet-to-wallet transfer.
     * Returns 201 Created for new transfers and 200 OK for idempotent replays.
     */
    @PostMapping
    public ResponseEntity<TransferResponse> create(@Valid @RequestBody CreateTransferRequest request) {
        TransferResponse response = transferService.execute(request);
        HttpStatus status = response.newlyCreated() ? HttpStatus.CREATED : HttpStatus.OK;
        return ResponseEntity.status(status).body(response);
    }
}

