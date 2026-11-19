package com.wallettransfer.repository;

import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletAlreadyExistsException;
import java.time.Instant;
import java.util.List;
import java.util.Optional;

/** Persistence port for wallets and balance changes. */
public interface WalletRepository {

  /**
   * Inserts a wallet.
   *
   * @throws WalletAlreadyExistsException if the id is taken
   */
  void create(Wallet wallet);

  Optional<Wallet> findById(String id);

  /**
   * Takes row locks on the given wallets in ascending id order (so concurrent transfers in opposite
   * directions cannot deadlock) and returns the ids that exist, in that order. Locks are held until
   * the surrounding transaction ends.
   */
  List<String> lockForUpdate(String firstId, String secondId);

  /**
   * Atomically subtracts {@code amount} only if the balance covers it. The funds check and the
   * decrement are a single statement, so concurrent debits cannot both observe the old balance.
   *
   * @return {@code true} if the wallet was debited, {@code false} if it is missing or short of
   *     funds
   */
  boolean debitIfSufficient(String id, long amount, Instant now);

  /** Atomically adds {@code amount} to the wallet. */
  void credit(String id, long amount, Instant now);
}
