package com.example.wallet.domain;

import jakarta.persistence.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;

@Entity
@Table(name = "wallets")
public class Wallet {

    @Id
    @Column(name = "wallet_id", length = 100, nullable = false)
    private String walletId;

    @Column(
            name = "balance",
            precision = 19,
            scale = 2,
            nullable = false
    )
    private BigDecimal balance;

    @Column(name = "status", length = 20, nullable = false)
    private String status;

    @Version
    @Column(name = "version", nullable = false)
    private Long version;

    @Column(name = "created_at", nullable = false, updatable = false)
    private LocalDateTime createdAt;

    @Column(name = "updated_at", nullable = false)
    private LocalDateTime updatedAt;

    /**
     * Required by JPA.
     */
    protected Wallet() {
    }

    /**
     * Constructor used when creating a new wallet.
     */
    public Wallet(
            String walletId,
            BigDecimal balance,
            String status
    ) {
        this.walletId = walletId;
        this.balance = balance;
        this.status = status;
    }

    @PrePersist
    protected void onCreate() {
        LocalDateTime now = LocalDateTime.now();

        if (version == null) {
            version = 0L;
        }

        if (createdAt == null) {
            createdAt = now;
        }

        if (updatedAt == null) {
            updatedAt = now;
        }
    }

    @PreUpdate
    protected void onUpdate() {
        updatedAt = LocalDateTime.now();
    }

    public String getWalletId() {
        return walletId;
    }

    public BigDecimal getBalance() {
        return balance;
    }

    public String getStatus() {
        return status;
    }

    public Long getVersion() {
        return version;
    }

    public LocalDateTime getCreatedAt() {
        return createdAt;
    }

    public LocalDateTime getUpdatedAt() {
        return updatedAt;
    }

    // -------------------------
    // Domain behavior
    // -------------------------

    public boolean isActive() {
        return "ACTIVE".equals(status);
    }

    public boolean hasSufficientBalance(BigDecimal amount) {
        return balance.compareTo(amount) >= 0;
    }
    /**
     * Adds money to the wallet.
     */
    public void credit(BigDecimal amount) {
        this.balance = this.balance.add(amount);
    }

    /**
     * Removes money from the wallet.
     */
    public void debit(BigDecimal amount) {
        this.balance = this.balance.subtract(amount);
    }
}