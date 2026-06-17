package com.wallettransfer.config;

import com.wallettransfer.wallet.domain.Wallet;
import com.wallettransfer.wallet.repository.WalletRepository;
import org.springframework.boot.CommandLineRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class DataInitializer {

    @Bean
    CommandLineRunner seedWallets(WalletRepository walletRepository) {
        return args -> {
            if (walletRepository.count() == 0) {
                walletRepository.save(new Wallet("wallet_1", 1000));
                walletRepository.save(new Wallet("wallet_2", 500));
                walletRepository.save(new Wallet("wallet_3", 250));
            }
        };
    }
}
