package com.wallet.controller;

import com.wallet.domain.TransferStatus;
import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.service.TransferService;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody TransferRequest request) {
        TransferExecutionResult result = transferService.executeTransfer(request);
        if (result.isNewTransfer()) {
            return ResponseEntity.status(HttpStatus.CREATED).body(result.transfer());
        }
        // Idempotent replay: mirror original outcome so callers see consistent HTTP semantics
        if (result.transfer().status() == TransferStatus.FAILED) {
            return ResponseEntity.status(HttpStatus.UNPROCESSABLE_ENTITY).body(result.transfer());
        }
        return ResponseEntity.ok(result.transfer());
    }

    @GetMapping("/{id}")
    public ResponseEntity<TransferResponse> getTransfer(@PathVariable UUID id) {
        return ResponseEntity.ok(transferService.getTransfer(id));
    }
}
