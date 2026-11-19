package com.wallettransfer.service;

import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletNotFoundException;
import com.wallettransfer.repository.WalletRepository;
import java.time.Clock;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** Creates wallets and reads balances. */
@Service
public class WalletService {

  private final WalletRepository wallets;
  private final Clock clock;

  WalletService(WalletRepository wallets, Clock clock) {
    this.wallets = wallets;
    this.clock = clock;
  }

  /** Registers a wallet funded with {@code openingBalance} (funded from outside the system). */
  @Transactional
  public Wallet create(String id, long openingBalance) {
    Wallet wallet = Wallet.open(id, openingBalance, clock.instant());
    wallets.create(wallet);
    return wallet;
  }

  @Transactional(readOnly = true)
  public Wallet get(String id) {
    return wallets.findById(id).orElseThrow(() -> new WalletNotFoundException(id));
  }
}
