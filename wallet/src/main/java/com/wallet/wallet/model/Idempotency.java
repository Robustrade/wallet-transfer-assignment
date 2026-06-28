package com.wallet.wallet.model;

import com.wallet.wallet.enums.TRANSACTION_STATUS;
import com.wallet.wallet.enums.TYPE;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

import java.util.Date;

@Entity
@Table
@Getter @Setter
@NoArgsConstructor
@AllArgsConstructor
public class Idempotency {

    @Id
    @Column
    String idempotencyKey;

    @Column
    TYPE method;

    @Column
    int responseCode;

    @Column
    String reponseJson;

    @Column
    Date date;

    @Column
    TRANSACTION_STATUS status;

    @Column
    String requestHash;

}
