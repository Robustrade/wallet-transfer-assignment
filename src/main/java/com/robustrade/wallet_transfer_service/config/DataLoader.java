package com.robustrade.wallet_transfer_service.config;

import com.robustrade.wallet_transfer_service.entity.Wallet;
import com.robustrade.wallet_transfer_service.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.boot.CommandLineRunner;
import org.springframework.stereotype.Component;

@Component
@RequiredArgsConstructor
public class DataLoader implements CommandLineRunner {

    private final WalletRepository walletRepository;

    @Override
    public void run(String... args) {

        walletRepository.save(
                Wallet.builder()
                        .balance(1000L)
                        .build());

        walletRepository.save(
                Wallet.builder()
                        .balance(1000L)
                        .build());

        System.out.println("Sample wallets created");
    }
}