package com.wallet.handler;

import java.util.UUID;

import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.wallet.service.TransferResult;
import com.wallet.service.TransferService;

import jakarta.validation.Valid;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    @PostMapping
    public ResponseEntity<TransferResponse> create(@Valid @RequestBody TransferRequest request) {
        UUID fromWalletId = RequestIds.parseUuid(request.fromWalletId());
        UUID toWalletId = RequestIds.parseUuid(request.toWalletId());

        TransferResult result = transferService.transfer(request.idempotencyKey(), fromWalletId, toWalletId,
                request.amount());

        HttpStatus status = result.isReplay() ? HttpStatus.OK : HttpStatus.CREATED;
        return ResponseEntity.status(status).body(TransferResponse.from(result.getTransfer()));
    }

    @GetMapping("/{id}")
    public ResponseEntity<TransferResponse> get(@PathVariable String id) {
        UUID transferId = RequestIds.parseUuid(id);
        return ResponseEntity.ok(TransferResponse.from(transferService.getTransfer(transferId)));
    }
}
