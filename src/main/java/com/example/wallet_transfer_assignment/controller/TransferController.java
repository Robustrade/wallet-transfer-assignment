package com.example.wallet_transfer_assignment.controller;

import com.example.wallet_transfer_assignment.dto.TransferRequest;
import com.example.wallet_transfer_assignment.dto.TransferResponse;
import com.example.wallet_transfer_assignment.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {

    private final TransferService transferService;

    @PostMapping
    @ResponseStatus(HttpStatus.OK)
    public TransferResponse createTransfer(
            @Valid @RequestBody TransferRequest request) {

        return transferService.transfer(request);
    }
}