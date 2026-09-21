package com.example.wallet.domain;

import jakarta.persistence.*;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(
        name = "transfers",
        uniqueConstraints = @UniqueConstraint(
                name = "uk_transfer_idempotency",
                columnNames = "idempotency_key"
        ),
        indexes = {
                @Index(
                        name = "idx_transfers_from_wallet_created",
                        columnList = "from_wallet_id, created_at DESC"
                ),
                @Index(
                        name = "idx_transfers_to_wallet_created",
                        columnList = "to_wallet_id, created_at DESC"
                )
        }
)
public class Transfer {
    @Id
    @Column(name = "transfer_id", nullable = false)
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, length = 200)
    private String idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false, length = 100)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, length = 100)
    private String toWalletId;

    @Column(nullable = false, precision = 19, scale = 2)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false, length = 20)
    private TransferStatus status;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "processed_at")
    private Instant processedAt;

    protected Transfer() {}

    public Transfer(String idempotencyKey, String fromWalletId, String toWalletId, BigDecimal amount) {
        this.id = UUID.randomUUID();
        this.idempotencyKey = idempotencyKey;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = TransferStatus.PENDING;
        this.createdAt = Instant.now();
    }

    public UUID getId() { return id; }
    public String getIdempotencyKey() { return idempotencyKey; }
    public String getFromWalletId() { return fromWalletId; }
    public String getToWalletId() { return toWalletId; }
    public BigDecimal getAmount() { return amount; }
    public TransferStatus getStatus() { return status; }
    public void markProcessed() { status = TransferStatus.PROCESSED; processedAt = Instant.now(); }
    public void markFailed() { status = TransferStatus.FAILED; }
}
