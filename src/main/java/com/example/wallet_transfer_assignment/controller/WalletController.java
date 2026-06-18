package com.example.wallet_transfer_assignment.controller;

import com.example.wallet_transfer_assignment.dto.BalanceResponse;
import com.example.wallet_transfer_assignment.dto.CreateWalletRequest;
import com.example.wallet_transfer_assignment.dto.TransactionHistoryResponse;
import com.example.wallet_transfer_assignment.dto.WalletResponse;
import com.example.wallet_transfer_assignment.service.WalletService;
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

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public WalletResponse createWallet(
            @Valid @RequestBody
            CreateWalletRequest request) {

        return walletService.createWallet(request);
    }

    @GetMapping("/{walletId}/balance")
    public BalanceResponse getBalance(
            @PathVariable UUID walletId) {

        return walletService.getBalance(walletId);
    }

    @GetMapping("/{walletId}/transactions")
    public List<TransactionHistoryResponse>
    getTransactionHistory(
            @PathVariable UUID walletId) {

        return walletService
                .getTransactionHistory(walletId);
    }
}