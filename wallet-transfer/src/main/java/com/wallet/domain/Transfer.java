package com.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.Getter;
import lombok.NoArgsConstructor;
import org.hibernate.annotations.UpdateTimestamp;

@Entity
@Table(name = "transfers")
@Getter
@NoArgsConstructor
public class Transfer {

    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, unique = true, length = 255)
    private String idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false, length = 50)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, length = 50)
    private String toWalletId;

    @Column(nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false, length = 20)
    private TransferStatus status;

    @Column(name = "failure_reason")
    private String failureReason;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public static Transfer pending(String idempotencyKey, String fromWalletId, String toWalletId, BigDecimal amount) {
        Transfer transfer = new Transfer();
        transfer.idempotencyKey = idempotencyKey;
        transfer.fromWalletId = fromWalletId;
        transfer.toWalletId = toWalletId;
        transfer.amount = amount;
        transfer.status = TransferStatus.PENDING;
        transfer.createdAt = Instant.now();
        transfer.updatedAt = Instant.now();
        return transfer;
    }

    public void markProcessed() {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                "Cannot mark transfer as PROCESSED from state: " + this.status);
        }
        this.status = TransferStatus.PROCESSED;
        this.updatedAt = Instant.now();
    }

    public void markFailed(String reason) {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                "Cannot mark transfer as FAILED from state: " + this.status);
        }
        this.status = TransferStatus.FAILED;
        this.failureReason = reason;
        this.updatedAt = Instant.now();
    }

    public boolean isPending() {
        return TransferStatus.PENDING == this.status;
    }
}
