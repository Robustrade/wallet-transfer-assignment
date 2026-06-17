package com.siddharth.wallet.config;

import com.siddharth.wallet.model.Wallet;
import com.siddharth.wallet.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.boot.CommandLineRunner;
import org.springframework.stereotype.Component;

import java.math.BigDecimal;
import java.util.UUID;

@Component
@RequiredArgsConstructor
public class DataInitializer implements CommandLineRunner {

    private final WalletRepository walletRepository;

    @Override
    public void run(String... args) {

        if (walletRepository.count() > 0) {
            return;
        }

        Wallet wallet1 = new Wallet();
        wallet1.setId(UUID.randomUUID());
        System.out.println("Wallet 1:"+ wallet1.getId());
        wallet1.setBalance(BigDecimal.valueOf(1000));

        Wallet wallet2 = new Wallet();
        wallet2.setId(UUID.randomUUID());
        System.out.println("Wallet 1:"+ wallet1.getId());
        wallet2.setBalance(BigDecimal.valueOf(1000));

        walletRepository.save(wallet1);
        walletRepository.save(wallet2);

        System.out.println("Wallet 1 = " + wallet1.getId());
        System.out.println("Wallet 2 = " + wallet2.getId());
    }
}
