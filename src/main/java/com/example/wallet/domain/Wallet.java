package com.example.wallet.domain;

import java.time.Instant;

public record Wallet(String id, long balance, Instant createdAt, Instant updatedAt) {

    public boolean canCover(long amount) {
        return balance >= amount;
    }
}
