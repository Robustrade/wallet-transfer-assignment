package com.rajat.wallet.domain.entities;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.math.BigDecimal;
import org.junit.jupiter.api.Test;

/**
 * Pure unit tests for the {@link Wallet} balance invariants — the last line of defence that
 * protects against overdraft even if a caller forgets to check funds. No Spring, no database.
 */
class WalletTest {

  @Test
  void debitReducesBalance() {
    Wallet wallet = new Wallet(new BigDecimal("100.00"), "INR");

    wallet.debit(new BigDecimal("30.00"));

    assertThat(wallet.getBalance()).isEqualByComparingTo("70.00");
  }

  @Test
  void creditIncreasesBalance() {
    Wallet wallet = new Wallet(new BigDecimal("100.00"), "INR");

    wallet.credit(new BigDecimal("25.50"));

    assertThat(wallet.getBalance()).isEqualByComparingTo("125.50");
  }

  @Test
  void debitOfExactBalanceIsAllowed() {
    Wallet wallet = new Wallet(new BigDecimal("50.00"), "INR");

    wallet.debit(new BigDecimal("50.00"));

    assertThat(wallet.getBalance()).isEqualByComparingTo("0.00");
  }

  @Test
  void debitBeyondBalanceIsRejectedAndLeavesBalanceUntouched() {
    Wallet wallet = new Wallet(new BigDecimal("40.00"), "INR");

    assertThatThrownBy(() -> wallet.debit(new BigDecimal("40.01")))
        .isInstanceOf(IllegalStateException.class);

    assertThat(wallet.getBalance()).isEqualByComparingTo("40.00");
  }

  @Test
  void hasSufficientFundsIsInclusiveOfTheExactAmount() {
    Wallet wallet = new Wallet(new BigDecimal("10.00"), "INR");

    assertThat(wallet.hasSufficientFunds(new BigDecimal("10.00"))).isTrue();
    assertThat(wallet.hasSufficientFunds(new BigDecimal("10.01"))).isFalse();
  }
}
