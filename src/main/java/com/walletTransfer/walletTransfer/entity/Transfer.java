package com.walletTransfer.walletTransfer.entity;


import com.walletTransfer.walletTransfer.enums.TransferStatus;
import jakarta.persistence.*;
import lombok.*;


import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

@Entity
@Table(name = "transfers")
@Getter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Transfer {
    @Id
    private UUID id;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(
            name = "from_wallet_id",
            nullable = false
    )
    private Wallet fromWallet;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(
            name = "to_wallet_id",
            nullable = false
    )
    private Wallet toWallet;

    @Column(nullable = false)
    private Long amount;

    @Column(name = "failure_reason")
    private String failureReason;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private TransferStatus status;

    @Column(name = "created_at", nullable = false)
    private LocalDateTime createdAt;

    @Column(name="updated_at", nullable = false)
    private LocalDateTime updatedAt;

    public void markProcessed() {

        if (status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                    "Only pending transfer can be processed"
            );
        }

        this.status = TransferStatus.PROCESSED;
    }

    public void markFailed(String failureReason) {
        this.failureReason = failureReason;

        if (status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                    "Only pending transfer can be failed"
            );
        }

        this.status = TransferStatus.FAILED;
    }

    @PrePersist
    public void prePersist() {

        if (createdAt == null) {
            createdAt = LocalDateTime.now();
        }
        updatedAt= LocalDateTime.now();

        if (status == null) {
            status = TransferStatus.PENDING;
        }
    }

    @PreUpdate
    public void preUpdate() {
        updatedAt = LocalDateTime.now();
    }
}
