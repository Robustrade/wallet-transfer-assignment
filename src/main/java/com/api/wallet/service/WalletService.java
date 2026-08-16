package com.api.wallet.service;

import com.api.wallet.dto.WalletResponse;
import com.api.wallet.exception.WalletNotFoundException;
import com.api.wallet.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletService {

    private final WalletRepository walletRepository;

    public WalletService(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    @Transactional(readOnly = true)
    public WalletResponse getBalance(String walletId) {
        return walletRepository
                .findById(walletId)
                .map(WalletResponse::from)
                .orElseThrow(() -> new WalletNotFoundException(walletId));
    }
}
