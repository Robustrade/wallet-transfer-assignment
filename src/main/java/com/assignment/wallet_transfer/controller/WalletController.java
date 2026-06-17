package com.assignment.wallet_transfer.controller;

import com.assignment.wallet_transfer.dtos.CreateWalletRequest;
import com.assignment.wallet_transfer.dtos.TransferHistoryResponse;
import com.assignment.wallet_transfer.dtos.WalletResponse;
import com.assignment.wallet_transfer.service.TransferService;
import com.assignment.wallet_transfer.service.WalletService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.UUID;

@RestController
@RequestMapping("/wallets")
@RequiredArgsConstructor
public class WalletController {

    private final WalletService walletService;

    private final TransferService transferService;

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public com.assignment.wallet_transfer.dtos.WalletResponse createWallet(
            @RequestBody @Valid CreateWalletRequest request
    ) {

        return walletService.createWallet(request);
    }

    @GetMapping("/{walletId}")
    public com.assignment.wallet_transfer.dtos.WalletResponse getWallet(
            @PathVariable UUID walletId
    ) {

        return walletService.getWallet(walletId);
    }


    @GetMapping("/{walletId}/transfers")
    public List<TransferHistoryResponse> getTransferHistory(
            @PathVariable UUID walletId) {

        return transferService.getWalletTransfers(walletId);
    }
}
