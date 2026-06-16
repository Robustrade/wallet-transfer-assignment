package com.wallet.domain;

import java.time.Instant;

/**
 * User wallet. Balance is stored in cents to avoid floating-point precision
 * issues. Concurrent mutations are coordinated by {@code SELECT ... FOR UPDATE}
 * in the repository, ordered by wallet id to prevent deadlocks.
 */
public class Wallet {

    private String id;
    private Long balance;
    private Instant createdAt;
    private Instant updatedAt;

    public Wallet(String id, Long balance) {
        this.id = id;
        this.balance = balance;
        this.createdAt = Instant.now();
        this.updatedAt = Instant.now();
    }

    /** Reconstruction constructor for repository loading. */
    public Wallet(String id, Long balance, Instant createdAt, Instant updatedAt) {
        this.id = id;
        this.balance = balance;
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
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

    /*
     * Defense-in-depth invariants. On the live request path the amount is
     * already non-negative (DTO @Positive validation) and the balance check
     * has run against the FOR-UPDATE-locked row in TransferExecutionService.
     * These guards exist to keep the entity correct in any future call site
     * that bypasses the service layer (tests, scripts, refactors).
     *
     * NOTE: updatedAt is intentionally NOT touched here. The persistence
     * layer is the authoritative clock — WalletRepository.save reads the
     * post-UPDATE timestamp via RETURNING and rebuilds the entity with it.
     * Setting an in-memory updatedAt here would either be silently overwritten
     * or, worse, suggest the domain owns the timestamp when it does not.
     */

    public void debit(long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Debit amount must be positive");
        }
        if (this.balance < amount) {
            throw new IllegalStateException(
                    "Insufficient balance: wallet " + id + " has " + balance + " but needs " + amount);
        }
        this.balance -= amount;
    }

    public void credit(long amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("Credit amount must be positive");
        }
        this.balance += amount;
    }
}
