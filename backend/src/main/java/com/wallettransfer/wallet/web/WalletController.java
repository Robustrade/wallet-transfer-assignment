package com.wallettransfer.wallet.web;

import com.wallettransfer.wallet.dto.CreateWalletRequest;
import com.wallettransfer.wallet.dto.WalletHistoryResponse;
import com.wallettransfer.wallet.dto.WalletResponse;
import com.wallettransfer.wallet.service.WalletService;
import jakarta.validation.Valid;
import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/wallets")
public class WalletController {

    private final WalletService walletService;

    public WalletController(WalletService walletService) {
        this.walletService = walletService;
    }

    @GetMapping
    public List<WalletResponse> listWallets() {
        return walletService.listWallets();
    }

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public WalletResponse createWallet(@Valid @RequestBody CreateWalletRequest request) {
        return walletService.createWallet(request);
    }

    @GetMapping("/{walletId}/history")
    public WalletHistoryResponse getWalletHistory(@PathVariable String walletId) {
        return walletService.getWalletHistory(walletId);
    }
}
