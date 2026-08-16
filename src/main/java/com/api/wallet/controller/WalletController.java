package com.api.wallet.controller;

import com.api.wallet.dto.TransferResponse;
import com.api.wallet.dto.WalletResponse;
import com.api.wallet.service.TransferService;
import com.api.wallet.service.WalletService;
import java.util.List;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletService walletService;
    private final TransferService transferService;

    public WalletController(WalletService walletService, TransferService transferService) {
        this.walletService = walletService;
        this.transferService = transferService;
    }

    @GetMapping("/{walletId}")
    public WalletResponse getWallet(@PathVariable String walletId) {
        return walletService.getBalance(walletId);
    }

    @GetMapping("/{walletId}/transfers")
    public List<TransferResponse> getTransferHistory(@PathVariable String walletId) {
        return transferService.getHistory(walletId);
    }
}
