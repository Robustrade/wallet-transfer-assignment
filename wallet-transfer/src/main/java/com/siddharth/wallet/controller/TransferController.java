package com.siddharth.wallet.controller;

import com.siddharth.wallet.dto.CreateTransferRequest;
import com.siddharth.wallet.dto.TransferResponse;
import com.siddharth.wallet.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {

    private final TransferService transferService;

    @PostMapping
    public TransferResponse transfer(
            @Valid
            @RequestBody
            CreateTransferRequest request) {

        return transferService.transfer(request);
    }
}