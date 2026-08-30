package com.example.wallettransfer.controller;

import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;
import com.example.wallettransfer.service.TransferService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
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
    public ResponseEntity<TransferResponse> createTransfer(
            @Valid @RequestBody CreateTransferRequest request
    ) {
        return ResponseEntity.ok(transferService.createTransfer(request));
    }
}
