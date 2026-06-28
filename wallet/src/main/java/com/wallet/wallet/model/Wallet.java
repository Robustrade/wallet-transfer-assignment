package com.wallet.wallet.model;

import jakarta.persistence.*;
import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

import java.time.LocalDateTime;
import java.util.Date;

@Entity
@Table(name="wallet")
@Getter @Setter
@NoArgsConstructor @AllArgsConstructor
public class Wallet {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    @Column
    int walletId;

    @Column
    String name;

    @Column
    double availableBalance;

    @Column
    double totalBalance;

    @Column
    double blockAmount;

    @Column
    String mobile;

    @Column
    String email;

    @Column
    LocalDateTime createdAt;

    @Column
    boolean isWalletLocked;
}
