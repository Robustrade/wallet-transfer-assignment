package com.wallet.service;

import java.util.UUID;

import org.springframework.stereotype.Service;

import com.wallet.domain.Wallet;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.WalletRepository;

@Service
public class WalletService {

    private final WalletRepository walletRepository;

    public WalletService(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    public Wallet getWallet(UUID id) {
        return walletRepository.findById(id).orElseThrow(() -> new WalletNotFoundException(id));
    }
}
