package com.robustrade.wallet.entities;

import com.robustrade.wallet.enums.LedgerEntryType;
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
import org.springframework.data.domain.Persistable;

/** Append-only. A correction is a new transfer in the other direction, never an edit. */
@Entity
@Table(name = "ledger_entries")
public class LedgerEntry implements Persistable<UUID> {

  @Id private UUID id;

  @Column(name = "transfer_id", nullable = false)
  private UUID transferId;

  @Column(name = "customer_id", nullable = false)
  private UUID customerId;

  @Column(name = "wallet_id", nullable = false)
  private String walletId;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false)
  private LedgerEntryType type;

  @Column(nullable = false)
  private BigDecimal amount;

  @CreationTimestamp
  @Column(name = "created_at", updatable = false)
  private Instant createdAt;

  protected LedgerEntry() {}

  public LedgerEntry(
      UUID transferId, UUID customerId, String walletId, LedgerEntryType type, BigDecimal amount) {
    this.id = UUID.randomUUID();
    this.transferId = transferId;
    this.customerId = customerId;
    this.walletId = walletId;
    this.type = type;
    this.amount = amount;
  }

  /**
   * The id is assigned in the constructor, so Spring Data would otherwise read every entry as
   * detached and issue a SELECT before each INSERT. Entries are append-only and saved exactly once,
   * so an unset createdAt (@CreationTimestamp fills it on insert) means "not yet stored".
   */
  @Override
  public boolean isNew() {
    return createdAt == null;
  }

  @Override
  public UUID getId() {
    return id;
  }

  public UUID getTransferId() {
    return transferId;
  }

  public UUID getCustomerId() {
    return customerId;
  }

  public String getWalletId() {
    return walletId;
  }

  public LedgerEntryType getType() {
    return type;
  }

  public BigDecimal getAmount() {
    return amount;
  }

  public Instant getCreatedAt() {
    return createdAt;
  }
}
