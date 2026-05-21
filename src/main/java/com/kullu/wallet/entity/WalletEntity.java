package com.kullu.wallet.entity;

import java.time.Instant;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;

import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

/**
 * A user wallet holding a non-negative integer balance in minor currency units.
 */
@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@Entity
@Table(name = "wallets")
public class WalletEntity {

    @Id
    @Column(name = "id", nullable = false, updatable = false)
    private String id;

    @Column(name = "balance", nullable = false)
    private long balance;

    @CreationTimestamp
    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public WalletEntity(final String id, final long balance) {
        if (id == null || id.isBlank()) {
            throw new IllegalArgumentException("wallet id must not be blank");
        }
        if (balance < 0) {
            throw new IllegalArgumentException("initial balance must be >= 0");
        }
        this.id = id;
        this.balance = balance;
    }

    public void debit(final long amount) {
        requirePositive(amount);
        if (balance < amount) {
            throw new IllegalStateException(
                "debit would overdraw wallet " + id + ": balance=" + balance + " amount=" + amount);
        }
        balance -= amount;
    }

    public void credit(final long amount) {
        requirePositive(amount);
        balance += amount;
    }

    private static void requirePositive(final long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("amount must be positive, got " + amount);
        }
    }
}
