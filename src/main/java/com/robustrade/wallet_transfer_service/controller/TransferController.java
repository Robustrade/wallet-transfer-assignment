package com.robustrade.wallet_transfer_service.controller;

import com.robustrade.wallet_transfer_service.dto.TransferRequest;
import com.robustrade.wallet_transfer_service.dto.TransferResponse;
import com.robustrade.wallet_transfer_service.service.TransferService;
import lombok.RequiredArgsConstructor;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {

    private final TransferService transferService;

    @PostMapping
    public TransferResponse transfer(
            @RequestBody TransferRequest request) {

        return transferService.transferMoney(request);
    }
}