package com.walletservice;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/**
 * Wallet Transfer Service Application.
 *
 * A reliable transactional service for wallet-to-wallet transfers with:
 * - Idempotency guarantees
 * - Concurrency safety
 * - Double-entry ledger consistency
 * - Safe state transitions
 */
@SpringBootApplication
public class WalletTransferApplication {
    public static void main(String[] args) {
        SpringApplication.run(WalletTransferApplication.class, args);
    }
}

