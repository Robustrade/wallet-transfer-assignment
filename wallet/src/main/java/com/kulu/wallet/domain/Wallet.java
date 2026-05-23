package com.kulu.wallet.domain;

import java.time.LocalDateTime;

public record Wallet(long id, long balance, LocalDateTime createdAt) {}
