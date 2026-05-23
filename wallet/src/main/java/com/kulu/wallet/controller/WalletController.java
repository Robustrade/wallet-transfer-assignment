package com.kulu.wallet.controller;

import com.kulu.wallet.dto.WalletBalanceResponse;
import com.kulu.wallet.exception.WalletNotFoundException;
import com.kulu.wallet.repository.WalletRepository;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class WalletController {

    @Autowired
    private WalletRepository walletRepo;

    @GetMapping("/wallets/{id}/balance")
    public ResponseEntity<WalletBalanceResponse> getBalance(@PathVariable long id) {
        return walletRepo.findById(id)
            .map(w -> ResponseEntity.ok(new WalletBalanceResponse(w.id(), w.balance())))
            .orElseThrow(() -> new WalletNotFoundException(id));
    }
}
