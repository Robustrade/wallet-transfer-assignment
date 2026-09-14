package com.robustrade.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Index;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import lombok.AccessLevel;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

/**
 * Wallet aggregate. Indexes are declared here to document Flyway-owned schema
 * ({@code V3__query_performance_indexes.sql}); Hibernate does not create them ({@code ddl-auto=none}).
 */
@Entity
@Table(
        name = "wallets",
        indexes = {
                @Index(name = "wallets_user_id_uk", columnList = "user_id", unique = true)
        }
)
@Getter
@Setter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@AllArgsConstructor
@Builder
public class Wallet {

    @Id
    @Column(name = "id", nullable = false, length = 36)
    private String id;

    @Column(name = "user_id", nullable = false)
    private Long userId;

    @Column(name = "balance", nullable = false, precision = 15, scale = 2)
    private BigDecimal balance;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public static Wallet create(Long userId, BigDecimal initialBalance) {
        Instant now = Instant.now();
        return Wallet.builder()
                .id(java.util.UUID.randomUUID().toString())
                .userId(userId)
                .balance(initialBalance)
                .createdAt(now)
                .updatedAt(now)
                .build();
    }

    public boolean hasSufficientBalance(BigDecimal amount) {
        return balance != null && amount != null && balance.compareTo(amount) >= 0;
    }

    public void debit(BigDecimal amount) {
        requirePositiveAmount(amount);
        if (!hasSufficientBalance(amount)) {
            throw new IllegalArgumentException("Insufficient balance for wallet: " + id);
        }
        this.balance = balance.subtract(amount);
        this.updatedAt = Instant.now();
    }

    public void credit(BigDecimal amount) {
        requirePositiveAmount(amount);
        this.balance = balance.add(amount);
        this.updatedAt = Instant.now();
    }

    private static void requirePositiveAmount(BigDecimal amount) {
        if (amount == null || amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Amount must be greater than 0");
        }
    }
}
