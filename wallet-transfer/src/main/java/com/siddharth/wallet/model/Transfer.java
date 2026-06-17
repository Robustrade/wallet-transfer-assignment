package com.siddharth.wallet.model;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import lombok.Data;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Data
public class Transfer {

    @Id
    UUID id;

    @Column(nullable = false, unique = true)
    private String idempotencyKey;

    @Column(nullable = false)
    UUID fromWalletId;

    @Column(nullable = false)
    UUID toWalletId;

    @Column(nullable = false, precision = 19, scale = 2)
    BigDecimal amount;

    Instant createdAt;

    TransferStatus status;
}