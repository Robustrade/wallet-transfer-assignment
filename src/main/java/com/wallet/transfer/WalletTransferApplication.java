package com.wallet.transfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/**
 * Main entry point for the Wallet Transfer Spring Boot application.
 * This class boots up the application context and starts the embedded Tomcat server.
 */
@SpringBootApplication
public class WalletTransferApplication {

    /**
     * The main execution method that starts the Spring Boot application.
     *
     * @param args Command-line arguments passed to the application.
     */
    public static void main(String[] args) {
        SpringApplication.run(WalletTransferApplication.class, args);
    }
}
