package com.example.wallet.handler;

import com.example.wallet.handler.dto.WalletBalanceResponse;
import com.example.wallet.model.Wallet;
import com.example.wallet.service.WalletService;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/wallets")
@RequiredArgsConstructor
public class WalletHandler {

    private final WalletService walletService;

    /**
     * GET /wallets/{walletId}/balance
     *
     * Returns the current balance of the specified wallet.
     */
    @GetMapping("/{walletId}/balance")
    public ResponseEntity<WalletBalanceResponse> getBalance(@PathVariable String walletId) {
        return walletService.getWallet(walletId)
                .map(wallet -> ResponseEntity.ok(toResponse(wallet)))
                .orElse(ResponseEntity.notFound().build());
    }

    private WalletBalanceResponse toResponse(Wallet wallet) {
        return new WalletBalanceResponse(wallet.id(), wallet.balance().toPlainString());
    }
}

