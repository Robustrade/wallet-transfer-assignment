package com.wallet.domain;

import java.time.Instant;

public record Wallet(String id, long balance, long version, Instant createdAt) {}
