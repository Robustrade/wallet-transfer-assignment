package com.rajat.wallet.domain.entities;

import com.rajat.wallet.domain.entities.common.AuditableEntity;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import lombok.Getter;
import lombok.NoArgsConstructor;

/**
 * A wallet holding a materialized {@code balance}. The balance is updated in the same transaction
 * as the ledger entries it derives from; the invariant {@code balance == SUM(credits) -
 * SUM(debits)} always holds for a committed transaction. The entity exposes <b>getters only</b> —
 * no {@code @Setter} — so the balance can only change through {@link #debit(BigDecimal)} / {@link
 * #credit(BigDecimal)}, keeping the overdraft guard impossible to bypass.
 */
@Entity
@Table(name = "wallets")
@Getter
@NoArgsConstructor
public class Wallet extends AuditableEntity {

  @Column(nullable = false, precision = 19, scale = 2)
  private BigDecimal balance = BigDecimal.ZERO;

  @Column(nullable = false)
  private String currency = "INR";

  public Wallet(BigDecimal balance, String currency) {
    this.balance = balance;
    this.currency = currency;
  }

  /** Returns true if this wallet can cover the given amount. */
  public boolean hasSufficientFunds(BigDecimal amount) {
    return balance.compareTo(amount) >= 0;
  }

  /**
   * Subtracts {@code amount} from the balance. Callers must hold a lock and verify funds first;
   * this is the last line of defence and throws if it would go negative.
   */
  public void debit(BigDecimal amount) {
    if (!hasSufficientFunds(amount)) {
      throw new IllegalStateException("Debit would make wallet " + getId() + " balance negative");
    }
    this.balance = this.balance.subtract(amount);
  }

  /** Adds {@code amount} to the balance. */
  public void credit(BigDecimal amount) {
    this.balance = this.balance.add(amount);
  }
}
