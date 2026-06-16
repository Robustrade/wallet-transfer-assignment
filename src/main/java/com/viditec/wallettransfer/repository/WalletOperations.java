package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.dto.WalletDto;

import java.util.Optional;

public interface WalletOperations {

    void createWallet(String walletId, long initialBalance);

    Optional<WalletDto> findById(String walletId);

    boolean exists(String walletId);

    int debitIfSufficientFunds(String walletId, long amount);

    int credit(String walletId, long amount);
}
