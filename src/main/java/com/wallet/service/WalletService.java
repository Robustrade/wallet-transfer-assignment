package com.wallet.service;

import com.wallet.api.dto.WalletResponse;
import com.wallet.api.exception.WalletNotFoundException;
import com.wallet.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletService {

    private final WalletRepository walletRepository;

    public WalletService(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    @Transactional(readOnly = true)
    public WalletResponse getWallet(String walletId) {
        return walletRepository
                .findById(walletId)
                .map(wallet -> new WalletResponse(wallet.id(), wallet.balance()))
                .orElseThrow(() -> new WalletNotFoundException(walletId));
    }
}
