package com.wallet.domain;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import lombok.Getter;

@Getter
public class Wallet {

    private final UUID id;
    private final BigDecimal balance;
    private final Instant createdAt;
    private final Instant updatedAt;

    public Wallet(UUID id, BigDecimal balance, Instant createdAt, Instant updatedAt) {
        this.id = id;
        this.balance = Money.normalize(balance);
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
    }

    public boolean hasAtLeast(BigDecimal amount) {
        return balance.compareTo(amount) >= 0;
    }
}
