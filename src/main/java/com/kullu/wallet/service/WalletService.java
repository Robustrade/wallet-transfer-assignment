package com.kullu.wallet.service;

import java.util.Optional;

import org.springframework.stereotype.Service;

import com.kullu.wallet.entity.WalletEntity;
import com.kullu.wallet.exception.WalletNotFoundException;
import com.kullu.wallet.repository.WalletRepository;

@Service
public class WalletService {

    private final WalletRepository wallets;

    public WalletService(final WalletRepository wallets) {
        this.wallets = wallets;
    }

    /**
     * Acquire a {@code FOR UPDATE} row lock on the wallet, returning the
     * managed entity.
     *
     * @throws WalletNotFoundException if no wallet exists for the given id
     */
    public WalletEntity lockAndGet(final String id) {
        return wallets.lockById(id)
            .orElseThrow(() -> new WalletNotFoundException(id));
    }

    /**
     * Lookup a wallet without locking. Primarily for read-only scenarios.
     */
    public Optional<WalletEntity> findById(final String id) {
        return wallets.findById(id);
    }

    public WalletEntity save(final WalletEntity walletEntity) {
        return wallets.save(walletEntity);
    }
}
