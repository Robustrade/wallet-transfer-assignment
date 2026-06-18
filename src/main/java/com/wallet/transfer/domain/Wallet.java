package com.wallet.transfer.domain;

import jakarta.persistence.*;
import lombok.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Wallet Entity
 * 
 * Represents a user's wallet with balance tracking.
 * 
 * Key Design Decisions:
 * - id: Technical surrogate key (UUID)
 * - walletId: Business key (unique external identifier)
 * - balance: Maintained for query efficiency (also reconcilable from ledger)
 * - Balance must never be negative (enforced by CHECK constraint and validation)
 */
@Entity
@Table(
    name = "wallets",
    indexes = {
        @Index(name = "idx_wallets_wallet_id", columnList = "wallet_id")
    }
)
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Wallet {
    
    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;
    
    @Column(unique = true, nullable = false, length = 255)
    private String walletId;
    
    @Column(nullable = false, precision = 19, scale = 2)
    private BigDecimal balance;
    
    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;
    
    @Column(nullable = false)
    private LocalDateTime updatedAt;
    
    @PrePersist
    protected void onCreate() {
        createdAt = LocalDateTime.now();
        updatedAt = LocalDateTime.now();
    }
    
    @PreUpdate
    protected void onUpdate() {
        updatedAt = LocalDateTime.now();
    }
    
    /**
     * Validates that balance is non-negative
     * @throws IllegalArgumentException if balance is negative
     */
    public void validateBalance() {
        if (balance.compareTo(BigDecimal.ZERO) < 0) {
            throw new IllegalArgumentException("Balance cannot be negative");
        }
    }
    
    /**
     * Debits amount from wallet balance
     * @param amount the amount to debit
     * @throws IllegalArgumentException if amount is negative or exceeds balance
     */
    public void debit(BigDecimal amount) {
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Debit amount must be positive");
        }
        if (amount.compareTo(this.balance) > 0) {
            throw new IllegalArgumentException("Insufficient balance");
        }
        this.balance = this.balance.subtract(amount);
    }
    
    /**
     * Credits amount to wallet balance
     * @param amount the amount to credit
     * @throws IllegalArgumentException if amount is negative or zero
     */
    public void credit(BigDecimal amount) {
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Credit amount must be positive");
        }
        this.balance = this.balance.add(amount);
    }
}
