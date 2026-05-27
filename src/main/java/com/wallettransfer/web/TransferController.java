package com.wallettransfer.web;

import com.wallettransfer.service.TransferService;
import com.wallettransfer.web.dto.CreateTransferRequest;
import com.wallettransfer.web.dto.TransferResponse;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

/**
 * HTTP API for wallet transfers (assignment endpoint: POST /transfers).
 * Delegates all business rules to {@link TransferService}.
 */
@RestController
@RequestMapping
public class TransferController {

    private final TransferService transferService;

    /**
     * @param transferService application transfer workflow
     */
    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    /**
     * Creates or replays a transfer. Same idempotency key returns the original result.
     *
     * @param request JSON body with idempotency key, wallets, and amount
     * @return transfer status and ledger entries
     */
    @PostMapping("/transfers")
    @ResponseStatus(HttpStatus.CREATED)
    public TransferResponse createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        return transferService.createTransfer(request);
    }
}
