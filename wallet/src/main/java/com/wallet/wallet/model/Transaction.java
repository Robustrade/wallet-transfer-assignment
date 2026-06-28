package com.wallet.wallet.model;

import com.wallet.wallet.enums.PAYMENT_STATUS;
import jakarta.persistence.*;
import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

import java.util.Date;

@Entity
@Table(name="transaction")
@Getter @Setter
@NoArgsConstructor @AllArgsConstructor
public class Transaction {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    @Column
    int transactionId;

    @Column
    int fromWalletId;

    @Column
    int toWalletId;

    @Column
    double amount;

    @Column
    PAYMENT_STATUS paymentStatus;

    @Column
    Date transactionDate;

}
