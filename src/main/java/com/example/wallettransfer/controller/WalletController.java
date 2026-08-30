package com.example.wallettransfer.controller;

import com.example.wallettransfer.dto.TransferHistoryResponse;
import com.example.wallettransfer.dto.WalletResponse;
import com.example.wallettransfer.service.WalletQueryService;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import lombok.RequiredArgsConstructor;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/wallets")
@RequiredArgsConstructor
public class WalletController {

    private final WalletQueryService walletQueryService;

    @GetMapping("/{walletId}")
    public WalletResponse getWallet(@PathVariable String walletId) {
        return walletQueryService.getWallet(walletId);
    }

    @GetMapping("/{walletId}/transfers")
    public TransferHistoryResponse getTransferHistory(
            @PathVariable String walletId,
            @RequestParam(defaultValue = "50") @Min(1) @Max(100) int limit,
            @RequestParam(required = false) String cursor
    ) {
        return walletQueryService.getTransferHistory(walletId, limit, cursor);
    }
}
