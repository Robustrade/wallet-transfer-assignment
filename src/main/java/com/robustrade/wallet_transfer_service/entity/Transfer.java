package com.robustrade.wallet_transfer_service.entity;

import com.robustrade.wallet_transfer_service.enums.TransferStatus;
import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;

@Entity
@Table(
    name = "transfers",
    uniqueConstraints = {
        @UniqueConstraint(columnNames = "idempotencyKey")
    }
)
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Transfer {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    private String idempotencyKey;

    private Long fromWalletId;

    private Long toWalletId;

    private Long amount;

    @Enumerated(EnumType.STRING)
    private TransferStatus status;

    private LocalDateTime createdAt;
}