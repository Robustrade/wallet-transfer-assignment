package com.siddharth.wallet.model;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Version;
import lombok.Data;

import java.math.BigDecimal;
import java.util.UUID;

@Entity
@Data
public class Wallet {

    @Id
    UUID id;

    @Column(nullable = false, precision = 19, scale = 2)
    BigDecimal balance;

    @Version
    Long version;
}
