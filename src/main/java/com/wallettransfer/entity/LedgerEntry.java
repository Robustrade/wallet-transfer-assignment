package com.wallettransfer.entity;

import com.wallettransfer.enums.LedgerEntryType;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.FetchType;
import jakarta.persistence.ForeignKey;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
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

@Entity
@Table(
    name = "ledger_entries",
    indexes = {
      @Index(name = "idx_ledger_transfer", columnList = "transfer_id"),
      @Index(name = "idx_ledger_wallet", columnList = "wallet_id")
    },
    uniqueConstraints = {
      @UniqueConstraint(
          name = "uq_ledger_transfer_wallet_type",
          columnNames = {"transfer_id", "wallet_id", "entry_type"})
    })
@Check(constraints = "amount > 0")
@Getter
@Setter
@AllArgsConstructor
@NoArgsConstructor
@Builder
public class LedgerEntry {

  @Id
  @GeneratedValue(strategy = GenerationType.IDENTITY)
  private Long id;

  @ManyToOne(optional = false, fetch = FetchType.LAZY)
  @JoinColumn(
      name = "wallet_id",
      nullable = false,
      foreignKey = @ForeignKey(name = "fk_ledger_wallet"))
  private Wallet wallet;

  @ManyToOne(optional = false, fetch = FetchType.LAZY)
  @JoinColumn(
      name = "transfer_id",
      nullable = false,
      foreignKey = @ForeignKey(name = "fk_ledger_transfer"))
  private Transfer transfer;

  @Enumerated(EnumType.STRING)
  @Column(name = "entry_type", nullable = false, length = 16)
  private LedgerEntryType entryType;

  @Column(name = "amount", nullable = false)
  private Long amount;

  @CreationTimestamp
  @Column(name = "created_at", nullable = false, updatable = false)
  private Instant createdAt;
}
