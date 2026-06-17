package com.assignment.wallet_transfer.service;


import com.assignment.wallet_transfer.dtos.CreateWalletRequest;
import com.assignment.wallet_transfer.dtos.WalletResponse;

import java.util.UUID;

public interface WalletService {

    com.assignment.wallet_transfer.dtos.WalletResponse createWallet(CreateWalletRequest request);

    WalletResponse getWallet(UUID walletId);

}