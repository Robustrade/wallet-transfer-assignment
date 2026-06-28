package com.wallet.wallet.model;

import com.wallet.wallet.enums.TYPE;
import jakarta.annotation.Nonnull;
import jakarta.persistence.*;
import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

@Entity
@Table(name="ledger_entry")
@Getter @Setter
@NoArgsConstructor @AllArgsConstructor
public class LedgerEntry {

    @Id
    @Column
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    int id;

    @Column
    int transactionId;

    @Column
    int walletId;

    @Column
    double amount;

    @Column
    TYPE type;
}
