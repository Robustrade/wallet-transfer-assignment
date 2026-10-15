package com.wallettransfer.entity;

import com.wallettransfer.enums.TransferStatus;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.FetchType;
import jakarta.persistence.ForeignKey;
import jakarta.persistence.Id;
import jakarta.persistence.Index;
import jakarta.persistence.JoinColumn;
import jakarta.persistence.ManyToOne;
import jakarta.persistence.Table;
import jakarta.persistence.UniqueConstraint;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;
import org.hibernate.annotations.Check;
import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

@Entity
@Table(
    name = "transfers",
    indexes = {
      @Index(name = "idx_transfers_from_wallet", columnList = "from_wallet_id"),
      @Index(name = "idx_transfers_to_wallet", columnList = "to_wallet_id"),
      @Index(name = "idx_transfers_status", columnList = "status")
    },
    uniqueConstraints = {
      @UniqueConstraint(
          name = "uq_transfers_idempotency_key",
          columnNames = {"idempotency_key"})
    })
@Check(constraints = "amount > 0")
@Check(constraints = "from_wallet_id <> to_wallet_id")
@Getter
@Setter
@AllArgsConstructor
@NoArgsConstructor
@Builder
public class Transfer {

  @Id
  @Column(name = "id", nullable = false, length = 36)
  private String id;

  @Column(name = "idempotency_key", nullable = false, length = 128)
  private String idempotencyKey;

  @ManyToOne(optional = false, fetch = FetchType.LAZY)
  @JoinColumn(
      name = "from_wallet_id",
      nullable = false,
      foreignKey = @ForeignKey(name = "fk_transfers_from_wallet"))
  private Wallet fromWallet;

  @ManyToOne(optional = false, fetch = FetchType.LAZY)
  @JoinColumn(
      name = "to_wallet_id",
      nullable = false,
      foreignKey = @ForeignKey(name = "fk_transfers_to_wallet"))
  private Wallet toWallet;

  @Column(name = "amount", nullable = false)
  private Long amount;

  @Enumerated(EnumType.STRING)
  @Column(name = "status", nullable = false, length = 32)
  private TransferStatus status;

  @Column(name = "failure_reason", length = 512)
  private String failureReason;

  @CreationTimestamp
  @Column(name = "created_at", nullable = false, updatable = false)
  private Instant createdAt;

  @UpdateTimestamp
  @Column(name = "updated_at", nullable = false)
  private Instant updatedAt;

  public void markProcessed() {
    if (status != TransferStatus.PENDING) {
      throw new IllegalStateException("Cannot mark transfer processed from status " + status);
    }
    status = TransferStatus.PROCESSED;
  }

  public void markFailed(String reason) {
    if (status != TransferStatus.PENDING) {
      throw new IllegalStateException("Cannot mark transfer failed from status " + status);
    }
    status = TransferStatus.FAILED;
    failureReason = reason;
  }
}
