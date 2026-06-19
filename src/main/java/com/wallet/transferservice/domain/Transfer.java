package com.wallet.transferservice.domain;

import com.wallet.transferservice.domain.enums.TransferStatus;
import jakarta.persistence.*;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "transfers")
public class Transfer {

    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, unique = true)
    private UUID idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false)
    private UUID fromWalletId;

    @Column(name = "to_wallet_id", nullable = false)
    private UUID toWalletId;

    @Column(name = "amount", nullable = false, precision = 18, scale = 2)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 20)
    private TransferStatus status = TransferStatus.COMPLETED;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @PrePersist
    protected void onCreate() {
        this.createdAt = Instant.now();
    }

    // ── Constructors ──────────────────────────────────────────

    public Transfer() {
    }

    public Transfer(UUID idempotencyKey, UUID fromWalletId, UUID toWalletId, BigDecimal amount) {
        this.idempotencyKey = idempotencyKey;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = TransferStatus.COMPLETED;
    }

    // ── Getters & Setters ─────────────────────────────────────

    public UUID getId() {
        return id;
    }

    public void setId(UUID id) {
        this.id = id;
    }

    public UUID getIdempotencyKey() {
        return idempotencyKey;
    }

    public void setIdempotencyKey(UUID idempotencyKey) {
        this.idempotencyKey = idempotencyKey;
    }

    public UUID getFromWalletId() {
        return fromWalletId;
    }

    public void setFromWalletId(UUID fromWalletId) {
        this.fromWalletId = fromWalletId;
    }

    public UUID getToWalletId() {
        return toWalletId;
    }

    public void setToWalletId(UUID toWalletId) {
        this.toWalletId = toWalletId;
    }

    public BigDecimal getAmount() {
        return amount;
    }

    public void setAmount(BigDecimal amount) {
        this.amount = amount;
    }

    public TransferStatus getStatus() {
        return status;
    }

    public void markFailed() {
        if (this.status == TransferStatus.COMPLETED) {
            throw new IllegalStateException("Cannot fail a completed transfer");
        }
        this.status = TransferStatus.FAILED;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}
