package com.example.wallet.config;

import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import org.springframework.boot.CommandLineRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Profile;

@Configuration
@Profile("!test")
public class DevDataInitializer {

    @Bean
    CommandLineRunner seedWallets(WalletRepository walletRepository) {
        return args -> {
            createWalletIfMissing(walletRepository, "wallet_1", new BigDecimal("1000.0000"));
            createWalletIfMissing(walletRepository, "wallet_2", new BigDecimal("500.0000"));
        };
    }

    private void createWalletIfMissing(WalletRepository walletRepository, String id, BigDecimal balance) {
        if (walletRepository.existsById(id)) {
            return;
        }
        walletRepository.save(new Wallet(id, balance));
    }
}
