package com.robustrade.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Index;
import jakarta.persistence.Table;
import jakarta.persistence.UniqueConstraint;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.AccessLevel;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

/**
 * Transfer aggregate. Indexes/constraints mirror Flyway ({@code V1} idempotency unique,
 * {@code V3} from/to wallet indexes); Hibernate {@code ddl-auto=none}.
 */
@Entity
@Table(
        name = "transfers",
        indexes = {
                @Index(name = "transfers_from_wallet_idx", columnList = "from_wallet"),
                @Index(name = "transfers_to_wallet_idx", columnList = "to_wallet")
        },
        uniqueConstraints = {
                @UniqueConstraint(
                        name = "transfers_idempotency_key_uk",
                        columnNames = "idempotency_key"
                )
        }
)
@Getter
@Setter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@AllArgsConstructor
@Builder
public class Transfer {

    @Id
    @Column(name = "id", nullable = false)
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, length = 128)
    private String idempotencyKey;

    @Column(name = "from_wallet", nullable = false, length = 36)
    private String fromWallet;

    @Column(name = "to_wallet", nullable = false, length = 36)
    private String toWallet;

    @Column(name = "amount", nullable = false, precision = 15, scale = 2)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "state", nullable = false, length = 32)
    private TransferState state;

    @Column(name = "failure_reason", length = 512)
    private String failureReason;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public static Transfer createPending(
            String idempotencyKey,
            String fromWallet,
            String toWallet,
            BigDecimal amount
    ) {
        Instant now = Instant.now();
        return Transfer.builder()
                .id(UUID.randomUUID())
                .idempotencyKey(idempotencyKey)
                .fromWallet(fromWallet)
                .toWallet(toWallet)
                .amount(amount)
                .state(TransferState.PENDING)
                .createdAt(now)
                .updatedAt(now)
                .build();
    }

    public void markProcessed() {
        if (state != TransferState.PENDING && state != TransferState.RETRY) {
            throw new IllegalStateException(
                    "Invalid state transition from " + state + " to PROCESSED"
            );
        }
        this.state = TransferState.PROCESSED;
        this.failureReason = null;
        this.updatedAt = Instant.now();
    }

    public void markFailed(String reason) {
        if (state != TransferState.PENDING && state != TransferState.RETRY) {
            throw new IllegalStateException(
                    "Invalid state transition from " + state + " to FAILED"
            );
        }
        this.state = TransferState.FAILED;
        this.failureReason = reason;
        this.updatedAt = Instant.now();
    }

    public void markRetry() {
        if (state != TransferState.FAILED) {
            throw new IllegalStateException(
                    "Invalid state transition from " + state + " to RETRY; only FAILED can retry"
            );
        }
        this.state = TransferState.RETRY;
        this.failureReason = null;
        this.updatedAt = Instant.now();
    }
}
