package com.walletservice.domain.entity;

import jakarta.persistence.*;
import lombok.Getter;
import lombok.Setter;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Wallet entity representing a user's wallet.
 *
 * Design notes:
 * - Uses optimistic locking with @Version for detecting concurrent modifications
 * - Maintains a stored balance for query efficiency (balance can also be derived from ledger)
 * - Balance changes must be atomic and tracked through ledger entries
 */
@Entity
@Getter
@Setter
@Table(name = "wallets")
public class Wallet {
    @Id
    private String id;

    @Column(nullable = false)
    private Long balance;

    /**
     * Optimistic locking version to ensure consistency under concurrent writes.
     * Incremented on every update to detect race conditions.
     */
    @Version
    @Column(nullable = false)
    private Long version;

    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;

    @Column(nullable = false)
    private LocalDateTime updatedAt;

    // Constructors
    public Wallet() {
    }

    public Wallet(String id, Long balance, Long version, LocalDateTime createdAt, LocalDateTime updatedAt) {
        this.id = id;
        this.balance = balance;
        this.version = version;
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
    }

    @PrePersist
    protected void onCreate() {
        this.id = UUID.randomUUID().toString();
        this.createdAt = LocalDateTime.now();
        this.updatedAt = LocalDateTime.now();
        if (this.balance == null) {
            this.balance = 0L;
        }
    }

    @PreUpdate
    protected void onUpdate() {
        this.updatedAt = LocalDateTime.now();
    }

    /**
     * Validates that the wallet has sufficient balance for a debit operation.
     * This is an additional safeguard; the actual enforcement happens at the database level.
     */
    public void validateSufficientBalance(Long amount) {
        if (this.balance < amount) {
            throw new IllegalStateException(
                String.format("Insufficient balance. Required: %d, Available: %d", amount, this.balance));
        }
    }

    /**
     * Credits the wallet. Called after ledger entry creation ensures atomicity.
     */
    public void credit(Long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Credit amount must be positive");
        }
        this.balance += amount;
    }

    /**
     * Debits the wallet. Called after ledger entry creation ensures atomicity.
     */
    public void debit(Long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Debit amount must be positive");
        }
        if (this.balance < amount) {
            throw new IllegalStateException(
                String.format("Insufficient balance. Required: %d, Available: %d", amount, this.balance));
        }
        this.balance -= amount;
    }

    // Builder pattern
    public static WalletBuilder builder() {
        return new WalletBuilder();
    }

    public static class WalletBuilder {
        private String id;
        private Long balance;
        private Long version;
        private LocalDateTime createdAt;
        private LocalDateTime updatedAt;

        public WalletBuilder id(String id) {
            this.id = id;
            return this;
        }

        public WalletBuilder balance(Long balance) {
            this.balance = balance;
            return this;
        }

        public WalletBuilder version(Long version) {
            this.version = version;
            return this;
        }

        public WalletBuilder createdAt(LocalDateTime createdAt) {
            this.createdAt = createdAt;
            return this;
        }

        public WalletBuilder updatedAt(LocalDateTime updatedAt) {
            this.updatedAt = updatedAt;
            return this;
        }

        public Wallet build() {
            Wallet wallet = new Wallet();
            wallet.id = this.id;
            wallet.balance = this.balance;
            wallet.version = this.version;
            wallet.createdAt = this.createdAt;
            wallet.updatedAt = this.updatedAt;
            return wallet;
        }
    }
}

