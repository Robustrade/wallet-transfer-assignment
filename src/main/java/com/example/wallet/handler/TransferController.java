package com.example.wallet.handler;

import com.example.wallet.domain.TransferStatus;
import com.example.wallet.service.CreateTransferCommand;
import com.example.wallet.service.TransferResult;
import com.example.wallet.service.TransferService;
import jakarta.validation.Valid;
import java.util.List;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

/** Thin transport layer: validate, map to a command, call the service, map the result. */
@RestController
public class TransferController {

    private final TransferService service;

    public TransferController(TransferService service) {
        this.service = service;
    }

    /**
     * 201 first execution (PROCESSED), 200 replay of a PROCESSED transfer, 422 for a FAILED transfer
     * (first call and replays alike, so a retry always sees the same status code and body).
     */
    @PostMapping("/transfers")
    public ResponseEntity<TransferResponse> create(@Valid @RequestBody CreateTransferRequest req) {
        TransferResult result = service.createTransfer(new CreateTransferCommand(
                req.idempotencyKey(), req.fromWalletId(), req.toWalletId(), req.amount()));

        HttpStatus status;
        if (result.transfer().status() == TransferStatus.FAILED) {
            status = HttpStatus.UNPROCESSABLE_ENTITY;
        } else {
            status = result.replayed() ? HttpStatus.OK : HttpStatus.CREATED;
        }
        return ResponseEntity.status(status).body(TransferResponse.from(result.transfer()));
    }

    @GetMapping("/transfers/{id}")
    public TransferResponse get(@PathVariable UUID id) {
        return TransferResponse.from(service.getTransfer(id));
    }

    @GetMapping("/transfers/{id}/ledger-entries")
    public List<LedgerEntryResponse> ledgerEntries(@PathVariable UUID id) {
        service.getTransfer(id);
        return service.getLedgerEntries(id).stream().map(LedgerEntryResponse::from).toList();
    }
}
