package com.wallettransfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;

/**
 * JPA entity representing a wallet with a stored balance.
 * Balance is updated atomically during transfers under row locks.
 */
@Entity
@Table(name = "wallets")
public class Wallet {

    @Id
    private String id;

    /** Current spendable balance; must remain non-negative. */
    @Column(nullable = false)
    private long balance;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    /** Required by JPA. */
    protected Wallet() {}

    /**
     * Creates a wallet with explicit timestamps (typically at insert time).
     */
    public Wallet(String id, long balance, Instant createdAt, Instant updatedAt) {
        this.id = id;
        this.balance = balance;
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
    }

    public String getId() {
        return id;
    }

    public long getBalance() {
        return balance;
    }

    public void setBalance(long balance) {
        this.balance = balance;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }

    public void setUpdatedAt(Instant updatedAt) {
        this.updatedAt = updatedAt;
    }
}
