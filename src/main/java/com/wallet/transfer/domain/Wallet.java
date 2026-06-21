package com.wallet.transfer.domain;

import com.wallet.transfer.exception.BalanceViolationException;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;
import java.time.Instant;

@Entity
@Table(name = "wallets")
public class Wallet {

    @Id
    private String id;

    @Column(nullable = false)
    private Long balance;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    protected Wallet() {
    }

    public Wallet(String id, Long balance) {
        this.id = id;
        this.balance = balance;
    }

    @PrePersist
    void onCreate() {
        this.createdAt = Instant.now();
        this.updatedAt = Instant.now();
    }

    @PreUpdate
    void onUpdate() {
        this.updatedAt = Instant.now();
    }

    public String getId() {
        return id;
    }

    public Long getBalance() {
        return balance;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }

    public void debit(Long amount) {
        if (amount <= 0) {
            throw new BalanceViolationException(this.id, "Debit amount must be positive");
        }
        if (this.balance < amount) {
            throw new BalanceViolationException(this.id,
                    "Insufficient funds in wallet " + this.id);
        }
        this.balance -= amount;
    }

    public void credit(Long amount) {
        if (amount <= 0) {
            throw new BalanceViolationException(this.id, "Credit amount must be positive");
        }
        if (Long.MAX_VALUE - this.balance < amount) {
            throw new BalanceViolationException(this.id,
                    "Credit would overflow wallet " + this.id + " balance");
        }
        this.balance += amount;
    }
}
