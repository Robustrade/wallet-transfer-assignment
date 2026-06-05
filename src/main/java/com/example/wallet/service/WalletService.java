package com.example.wallet.service;

import com.example.wallet.model.Wallet;

import java.util.Optional;

public interface WalletService {

    /**
     * Retrieve the current balance of a wallet.
     *
     * @param walletId the wallet identifier
     * @return the wallet, or empty if not found
     */
    Optional<Wallet> getWallet(String walletId);
}

