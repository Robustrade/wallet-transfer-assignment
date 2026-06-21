package com.wallet.transfer.controller;

import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.WalletRepository;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;
import java.util.List;

@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletRepository walletRepository;

    public WalletController(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    @GetMapping
    public ResponseEntity<List<Wallet>> getAllWallets() {
        return ResponseEntity.ok(walletRepository.findAll());
    }

    @GetMapping("/{id}")
    public ResponseEntity<Wallet> getWallet(@PathVariable String id) {
        Wallet wallet = walletRepository.findById(id)
                .orElseThrow(() -> new WalletNotFoundException("Wallet not found: " + id));
        return ResponseEntity.ok(wallet);
    }
}
