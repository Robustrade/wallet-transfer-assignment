package com.wallettransfer.domain;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.time.Instant;
import org.junit.jupiter.api.Test;

class WalletTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Test
  void balanceStartsAtOpeningBalance() {
    Wallet wallet = Wallet.open("wallet_1", 500, NOW);

    assertThat(wallet.id()).isEqualTo("wallet_1");
    assertThat(wallet.balance()).isEqualTo(500);
    assertThat(wallet.openingBalance()).isEqualTo(500);
    assertThat(wallet.createdAt()).isEqualTo(NOW);
    assertThat(wallet.updatedAt()).isEqualTo(NOW);
  }

  @Test
  void zeroOpeningBalanceIsAllowed() {
    assertThat(Wallet.open("wallet_1", 0, NOW).balance()).isZero();
  }

  @Test
  void rejectsMissingId() {
    assertThatThrownBy(() -> Wallet.open(" ", 0, NOW))
        .isInstanceOf(ValidationException.class)
        .hasMessage("wallet id is required");
  }

  @Test
  void rejectsNegativeOpeningBalance() {
    assertThatThrownBy(() -> Wallet.open("wallet_1", -1, NOW))
        .isInstanceOf(ValidationException.class)
        .hasMessage("opening balance must not be negative");
  }
}
