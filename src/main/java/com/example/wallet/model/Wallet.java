package com.example.wallet.model;

import java.math.BigDecimal;
import java.time.Instant;

public record Wallet(
        String id,
        BigDecimal balance,
        Instant createdAt
) {}

