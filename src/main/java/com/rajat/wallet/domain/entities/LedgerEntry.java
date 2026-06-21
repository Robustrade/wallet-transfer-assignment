package com.rajat.wallet.domain.entities;

import com.rajat.wallet.domain.entities.common.AuditableEntity;
import com.rajat.wallet.domain.enums.EntryType;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.util.UUID;
import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;

/**
 * An immutable, append-only ledger entry. Every transfer produces exactly two of these (a DEBIT on
 * the source wallet and a CREDIT on the destination). {@code balanceAfter} snapshots the wallet
 * balance immediately after this entry was applied. Rows are never updated or deleted.
 */
@Entity
@Table(name = "ledger_entries")
@Getter
@AllArgsConstructor
@NoArgsConstructor
public class LedgerEntry extends AuditableEntity {

  @Column(name = "wallet_id", nullable = false, updatable = false)
  private UUID walletId;

  @Column(name = "transfer_id", nullable = false, updatable = false)
  private UUID transferId;

  @Enumerated(EnumType.STRING)
  @Column(nullable = false, updatable = false)
  private EntryType type;

  @Column(nullable = false, updatable = false, precision = 19, scale = 2)
  private BigDecimal amount;

  @Column(name = "balance_after", nullable = false, updatable = false, precision = 19, scale = 2)
  private BigDecimal balanceAfter;
}
