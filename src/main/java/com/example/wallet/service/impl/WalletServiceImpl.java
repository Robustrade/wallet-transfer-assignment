package com.example.wallet.service.impl;

import com.example.wallet.model.Wallet;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.service.WalletService;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.util.Optional;

@Service
@RequiredArgsConstructor
public class WalletServiceImpl implements WalletService {

    private final WalletRepository walletRepository;

    @Override
    public Optional<Wallet> getWallet(String walletId) {
        return walletRepository.findById(walletId);
    }
}

