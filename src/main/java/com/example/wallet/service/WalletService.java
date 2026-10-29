package com.example.wallet.service;

import com.example.wallet.domain.InvalidTransferException;
import com.example.wallet.domain.Wallet;
import com.example.wallet.domain.WalletNotFoundException;
import com.example.wallet.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletService {

    private final WalletRepository wallets;

    public WalletService(WalletRepository wallets) {
        this.wallets = wallets;
    }

    @Transactional
    public Wallet createWallet(String id, long openingBalance) {
        if (openingBalance < 0) {
            throw new InvalidTransferException("opening balance must not be negative");
        }
        wallets.insert(id, openingBalance);
        return wallets.findById(id).orElseThrow(() -> new WalletNotFoundException(id));
    }

    @Transactional(readOnly = true)
    public Wallet getWallet(String id) {
        return wallets.findById(id).orElseThrow(() -> new WalletNotFoundException(id));
    }
}
