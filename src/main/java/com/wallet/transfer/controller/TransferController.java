package com.wallet.transfer.controller;

import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferState;
import com.wallet.transfer.dto.CreateTransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.service.TransferService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import java.util.List;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;
    private final TransferRepository transferRepository;

    public TransferController(TransferService transferService, TransferRepository transferRepository) {
        this.transferService = transferService;
        this.transferRepository = transferRepository;
    }

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        TransferResponse response = transferService.createTransfer(request);
        return ResponseEntity.ok(response);
    }

    @GetMapping
    public ResponseEntity<List<Transfer>> getAllTransfers(@RequestParam(required = false) TransferState state) {
        if (state != null) {
            return ResponseEntity.ok(transferRepository.findByState(state));
        }
        return ResponseEntity.ok(transferRepository.findAll());
    }

    @GetMapping("/{id}")
    public ResponseEntity<Transfer> getTransfer(@PathVariable String id) {
        return transferRepository.findById(id)
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }
}
