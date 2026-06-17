package com.robustrade.wallet_transfer_service.entity;

import com.robustrade.wallet_transfer_service.enums.LedgerType;
import jakarta.persistence.*;
import lombok.*;

@Entity
@Table(name = "ledger_entries")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    private Long transferId;

    private Long walletId;

    @Enumerated(EnumType.STRING)
    private LedgerType type;

    private Long amount;
}