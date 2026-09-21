package com.example.wallet.config;

import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.WalletRepository;
import org.springframework.boot.CommandLineRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.math.BigDecimal;

@Configuration
public class SeedData {

    @Bean
    CommandLineRunner seed(WalletRepository repo) {
        return args -> {
            if (repo.count() == 0) {

                repo.save(new Wallet(
                        "wallet_1",
                        new BigDecimal("1000.00"),
                        "ACTIVE"
                ));

                repo.save(new Wallet(
                        "wallet_2",
                        new BigDecimal("500.00"),
                        "ACTIVE"
                ));
            }
        };
    }
}