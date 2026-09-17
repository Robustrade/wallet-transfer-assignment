package com.robustrade.wallet.entities;

import com.robustrade.wallet.enums.WalletStatus;
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
@Table(name = "wallets")
public class Wallet {

  @Id private String id;

  @Column(name = "customer_id", nullable = false)
  private UUID customerId;

  private String name;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false)
  private WalletStatus status;

  @Column(nullable = false)
  private BigDecimal balance;

  private String meta;

  @CreationTimestamp
  @Column(name = "created_at", updatable = false)
  private Instant createdAt;

  @UpdateTimestamp
  @Column(name = "updated_at")
  private Instant updatedAt;

  protected Wallet() {}

  public boolean isActive() {
    return status == WalletStatus.ACTIVE;
  }

  /** True when this wallet can cover {@code amount}. Only meaningful while the row lock is held. */
  public boolean hasSufficientBalance(BigDecimal amount) {
    return balance.compareTo(amount) >= 0;
  }

  public void debit(BigDecimal amount) {
    this.balance = this.balance.subtract(amount);
  }

  public void credit(BigDecimal amount) {
    this.balance = this.balance.add(amount);
  }

  public String getId() {
    return id;
  }

  public UUID getCustomerId() {
    return customerId;
  }

  public WalletStatus getStatus() {
    return status;
  }

  public BigDecimal getBalance() {
    return balance;
  }

  public Instant getCreatedAt() {
    return createdAt;
  }

  public Instant getUpdatedAt() {
    return updatedAt;
  }
}
