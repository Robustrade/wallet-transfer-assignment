package com.robustrade.wallet.entities;

import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.TransferStatus;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

@Entity
@Table(name = "transfers")
public class Transfer {

  @Id private UUID id;

  @Column(name = "idempotency_key", nullable = false, unique = true)
  private String idempotencyKey;

  @Column(name = "from_wallet_id", nullable = false)
  private String fromWalletId;

  @Column(name = "to_wallet_id", nullable = false)
  private String toWalletId;

  @Column(name = "initiated_by", nullable = false)
  private UUID initiatedBy;

  @Column(nullable = false)
  private BigDecimal amount;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false)
  private TransferStatus status;

  @Enumerated(EnumType.STRING)
  @Column(name = "failure_reason")
  private FailureReason failureReason;

  @CreationTimestamp
  @Column(name = "created_at", updatable = false)
  private Instant createdAt;

  @UpdateTimestamp
  @Column(name = "updated_at")
  private Instant updatedAt;

  protected Transfer() {}

  /** Creates a new transfer in PENDING. PROCESSED and FAILED are only reachable via the marks. */
  public Transfer(
      String idempotencyKey,
      String fromWalletId,
      String toWalletId,
      UUID initiatedBy,
      BigDecimal amount) {
    this.id = UUID.randomUUID();
    this.idempotencyKey = idempotencyKey;
    this.fromWalletId = fromWalletId;
    this.toWalletId = toWalletId;
    this.initiatedBy = initiatedBy;
    this.amount = amount;
    this.status = TransferStatus.PENDING;
  }

  public void markProcessed() {
    requirePending();
    this.status = TransferStatus.PROCESSED;
  }

  public void markFailed(FailureReason reason) {
    requirePending();
    this.status = TransferStatus.FAILED;
    this.failureReason = reason;
  }

  /** PROCESSED and FAILED are terminal. Nothing moves out of them, including on retry. */
  private void requirePending() {
    if (status != TransferStatus.PENDING) {
      throw new IllegalStateException(
          "Transfer " + id + " is " + status + " and cannot change state");
    }
  }

  /** True when {@code other} describes the same transfer, for idempotent replay checks. */
  public boolean matches(String otherFromWalletId, String otherToWalletId, BigDecimal otherAmount) {
    // compareTo, not equals: NUMERIC(15,4) reads back as 100.0000 while the request parses
    // to 100, and BigDecimal.equals compares scale, so a valid replay would 409 incorrectly.
    return fromWalletId.equals(otherFromWalletId)
        && toWalletId.equals(otherToWalletId)
        && amount.compareTo(otherAmount) == 0;
  }

  public UUID getId() {
    return id;
  }

  public String getIdempotencyKey() {
    return idempotencyKey;
  }

  public String getFromWalletId() {
    return fromWalletId;
  }

  public String getToWalletId() {
    return toWalletId;
  }

  public UUID getInitiatedBy() {
    return initiatedBy;
  }

  public BigDecimal getAmount() {
    return amount;
  }

  public TransferStatus getStatus() {
    return status;
  }

  public FailureReason getFailureReason() {
    return failureReason;
  }

  public Instant getCreatedAt() {
    return createdAt;
  }

  public Instant getUpdatedAt() {
    return updatedAt;
  }
}
