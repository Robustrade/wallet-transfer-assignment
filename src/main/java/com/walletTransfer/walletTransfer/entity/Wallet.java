package com.walletTransfer.walletTransfer.entity;
import com.walletTransfer.walletTransfer.exception.InsufficientBalanceException;
import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;
import java.util.UUID;


@Entity
@Table(name = "wallets")
@Getter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Wallet {

    @Id
    @Column(name = "id", nullable = false, updatable = false)
    private UUID id;

    @Column(nullable = false)
    private Long balance;

    /**
     * Reserved for future optimistic locking support.
     *
     * Current implementation uses:
     * - Pessimistic locking
     * - Deterministic lock ordering
     * - Retry mechanism
     */
    @Version
    @Column(nullable = false)
    private Long version;

    @Column(name = "created_at", nullable = false)
    private LocalDateTime createdAt;

    @Column(name = "updated_at", nullable = false)
    private LocalDateTime updatedAt;

    public boolean hasSufficientBalance(Long amount) {
        return balance >= amount;
    }

    public void debit(Long amount) {

        validateAmount(amount);

        if (!hasSufficientBalance(amount)) {
            throw new InsufficientBalanceException(
                    "Insufficient balance"
            );
        }

        this.balance -= amount;
    }

    public void credit(Long amount) {

        validateAmount(amount);

        this.balance += amount;
    }

    private void validateAmount(Long amount) {

        if (amount == null || amount <= 0) {
            throw new IllegalArgumentException(
                    "Amount must be greater than zero"
            );
        }
    }

    @PrePersist
    public void prePersist() {

        if (createdAt == null) {
            createdAt = LocalDateTime.now();
        }
        updatedAt= LocalDateTime.now();

        if (version == null) {
            version = 0L;
        }
    }

    @PreUpdate
    public void preUpdate() {
        updatedAt = LocalDateTime.now();
    }
}
