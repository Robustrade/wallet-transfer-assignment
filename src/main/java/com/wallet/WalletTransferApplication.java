package com.wallet;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableScheduling;

@SpringBootApplication
@EnableScheduling
public class WalletTransferApplication {

    public static void main(String[] args) {
        SpringApplication.run(WalletTransferApplication.class, args);
    }
}
