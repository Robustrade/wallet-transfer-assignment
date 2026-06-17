package com.wallettransfer.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import jakarta.persistence.Version;
import java.time.Instant;

@Entity
@Table(name = "wallets")
public class Wallet {

    @Id
    @Column(length = 64)
    private String id;

    @Column(nullable = false)
    private long balance;

    @Version
    private long version;

    @Column(nullable = false)
    private Instant updatedAt = Instant.now();

    protected Wallet() {
    }

    public Wallet(String id, long balance) {
        this.id = id;
        this.balance = balance;
    }

    public String getId() {
        return id;
    }

    public long getBalance() {
        return balance;
    }

    public long getVersion() {
        return version;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }

    public void debit(long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Amount must be positive");
        }
        if (balance < amount) {
            throw new InsufficientBalanceException(id, balance, amount);
        }
        balance -= amount;
        updatedAt = Instant.now();
    }

    public void credit(long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Amount must be positive");
        }
        balance += amount;
        updatedAt = Instant.now();
    }
}
