package com.wallettransfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/**
 * Entry point for the wallet transfer service.
 * Boots Spring Boot with web, JPA, Flyway, and REST controllers.
 */
@SpringBootApplication
public class WalletTransferApplication {

    /**
     * Starts the Spring application context and embedded web server.
     *
     * @param args command-line arguments (unused)
     */
    public static void main(String[] args) {
        SpringApplication.run(WalletTransferApplication.class, args);
    }
}
