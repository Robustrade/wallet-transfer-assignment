package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.entities.Wallet;
import com.robustrade.wallet.entities.Wallets;
import org.junit.jupiter.api.Test;

class LockedWalletsTest {

  private static final Wallet FROM = Wallets.active("wallet_1", "1000");
  private static final Wallet TO = Wallets.active("wallet_2", "500");

  /** Roles, not lock order: from is always the source however the ids sort. */
  @Test
  void bindsEachWalletToItsRole() {
    LockedWallets wallets = new LockedWallets(FROM, TO);

    assertThat(wallets.from()).isSameAs(FROM);
    assertThat(wallets.to()).isSameAs(TO);
  }
}
