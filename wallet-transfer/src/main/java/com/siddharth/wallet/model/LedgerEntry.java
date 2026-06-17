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
public class LedgerEntry {

    @Id
    UUID id;

    @Column(nullable = false)
    UUID transferId;

    @Column(nullable = false)
    UUID walletId;

    EntryType type;

    Instant createdAt;

    @Column(nullable = false, precision = 19, scale = 2)
    BigDecimal amount;
}