package com.assignment.wallet_transfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;



@SpringBootApplication(scanBasePackages = {
        "com.assignment.wallet",
        "com.assignment.wallet_transfer"
})
public class WalletTransferApplication {
    public static void main(String[] args) {
        SpringApplication.run(WalletTransferApplication.class, args);
    }
}
