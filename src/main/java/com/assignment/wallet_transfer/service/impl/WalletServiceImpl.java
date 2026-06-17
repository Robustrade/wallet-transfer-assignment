package com.assignment.wallet_transfer.service.impl;


import com.assignment.wallet_transfer.entity.Wallet;
import com.assignment.wallet_transfer.exception.WalletNotFoundException;
import com.assignment.wallet_transfer.repository.WalletRepository;
import com.assignment.wallet_transfer.dtos.CreateWalletRequest;
import com.assignment.wallet_transfer.dtos.WalletResponse;
import com.assignment.wallet_transfer.entity.Wallet;
import com.assignment.wallet_transfer.repository.WalletRepository;
import com.assignment.wallet_transfer.service.WalletService;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.math.BigDecimal;
import java.util.UUID;

@Service
@RequiredArgsConstructor
public class WalletServiceImpl implements WalletService {


    private final com.assignment.wallet_transfer.repository.WalletRepository walletRepository;

    @Override
    public com.assignment.wallet_transfer.dtos.WalletResponse createWallet(com.assignment.wallet_transfer.dtos.CreateWalletRequest request) {

        com.assignment.wallet_transfer.entity.Wallet wallet = com.assignment.wallet_transfer.entity.Wallet.builder()
                .balance(
                        request.balance() == null
                                ? BigDecimal.ZERO
                                : request.balance()
                )
                .build();

        Wallet savedWallet = walletRepository.save(wallet);

        return new com.assignment.wallet_transfer.dtos.WalletResponse(
                savedWallet.getId(),
                savedWallet.getBalance()
        );
    }

    @Override
    public com.assignment.wallet_transfer.dtos.WalletResponse getWallet(UUID walletId) {

        Wallet wallet = walletRepository.findById(walletId)
                .orElseThrow(() ->
                        new WalletNotFoundException(
                                "Wallet not found with id: " + walletId));

        return new WalletResponse(
                wallet.getId(),
                wallet.getBalance()
        );
    }
}
