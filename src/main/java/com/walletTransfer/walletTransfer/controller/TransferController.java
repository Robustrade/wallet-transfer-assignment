package com.walletTransfer.walletTransfer.controller;

import com.walletTransfer.walletTransfer.dto.TransferRequest;
    import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {
    private final TransferService transferService;

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(
            @RequestBody @Valid TransferRequest request) {

        return ResponseEntity.ok(
                transferService.createTransfer(request));
    }
}
