package com.wallettransfer.repository.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletAlreadyExistsException;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

class JdbcWalletRepositoryTest extends PostgresIntegrationTest {

  private static final Instant NOW = Instant.parse("2026-01-02T03:04:05Z");

  @Autowired private WalletRepository wallets;

  private void create(String id, long balance) {
    wallets.create(Wallet.open(id, balance, NOW));
  }

  private long balanceOf(String id) {
    return wallets.findById(id).orElseThrow().balance();
  }

  @Test
  void createsAndFindsAWallet() {
    create("w1", 250);

    Wallet found = wallets.findById("w1").orElseThrow();

    assertThat(found.id()).isEqualTo("w1");
    assertThat(found.balance()).isEqualTo(250);
    assertThat(found.openingBalance()).isEqualTo(250);
    assertThat(found.createdAt()).isEqualTo(NOW);
  }

  @Test
  void rejectsDuplicateWalletId() {
    create("w1", 10);

    assertThatThrownBy(() -> create("w1", 99)).isInstanceOf(WalletAlreadyExistsException.class);
    assertThat(balanceOf("w1")).isEqualTo(10);
  }

  @Test
  void unknownWalletIsEmpty() {
    assertThat(wallets.findById("nope")).isEmpty();
  }

  @Test
  void debitReducesBalanceAndCanDrainItToExactlyZero() {
    create("w1", 100);

    assertThat(wallets.debitIfSufficient("w1", 30, NOW)).isTrue();
    assertThat(balanceOf("w1")).isEqualTo(70);
    assertThat(wallets.debitIfSufficient("w1", 70, NOW)).isTrue();
    assertThat(balanceOf("w1")).isZero();
  }

  @Test
  void debitIsRefusedWhenFundsAreInsufficientAndBalanceIsUntouched() {
    create("w1", 50);

    assertThat(wallets.debitIfSufficient("w1", 51, NOW)).isFalse();

    assertThat(balanceOf("w1")).isEqualTo(50);
  }

  @Test
  void debitOfUnknownWalletAffectsNothing() {
    assertThat(wallets.debitIfSufficient("nope", 1, NOW)).isFalse();
  }

  @Test
  void creditIncreasesBalance() {
    create("w1", 5);

    wallets.credit("w1", 20, NOW);

    assertThat(balanceOf("w1")).isEqualTo(25);
  }

  @Test
  void lockForUpdateReturnsExistingWalletsOrderedById() {
    create("b", 1);
    create("a", 2);

    List<String> locked = wallets.lockForUpdate("b", "a");

    assertThat(locked).containsExactly("a", "b");
  }

  @Test
  void lockForUpdateOmitsUnknownWallets() {
    create("a", 1);

    assertThat(wallets.lockForUpdate("a", "ghost")).containsExactly("a");
  }
}
