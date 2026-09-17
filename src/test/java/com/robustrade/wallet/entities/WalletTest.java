package com.robustrade.wallet.entities;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.enums.WalletStatus;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class WalletTest {

  @Test
  void onlyActiveWalletsAreActive() {
    assertThat(Wallets.active("wallet_1", "100").isActive()).isTrue();
    assertThat(status(WalletStatus.INACTIVE).isActive()).isFalse();
    assertThat(status(WalletStatus.BLOCKED).isActive()).isFalse();
  }

  @Test
  void sufficientBalanceCoversEqualAndGreaterAmounts() {
    Wallet wallet = Wallets.active("wallet_1", "100");

    assertThat(wallet.hasSufficientBalance(new BigDecimal("99.99"))).isTrue();
    assertThat(wallet.hasSufficientBalance(new BigDecimal("100"))).isTrue();
    assertThat(wallet.hasSufficientBalance(new BigDecimal("100.01"))).isFalse();
  }

  private static Wallet status(WalletStatus status) {
    return Wallets.wallet("wallet_1", UUID.randomUUID(), status, new BigDecimal("100"));
  }
}
