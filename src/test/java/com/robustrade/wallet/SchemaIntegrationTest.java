package com.robustrade.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.enums.WalletStatus;
import com.robustrade.wallet.repository.WalletRepository;
import com.robustrade.wallet.support.IntegrationTest;
import java.math.BigDecimal;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.transaction.annotation.Transactional;

/**
 * Boots the app against a real Postgres. Passing proves three things at once: the Flyway migrations
 * apply, the JPA entities match the resulting schema (ddl-auto=validate), and the seed data loads.
 */
@Tag("integration")
class SchemaIntegrationTest extends IntegrationTest {

  @Autowired private WalletRepository walletRepository;

  @Test
  void migrationsApplyAndSeedWalletsLoad() {
    assertThat(walletRepository.count()).isEqualTo(4);

    var wallet = walletRepository.findById("wallet_1").orElseThrow();
    assertThat(wallet.getStatus()).isEqualTo(WalletStatus.ACTIVE);
    assertThat(wallet.getBalance()).isEqualByComparingTo(new BigDecimal("1000"));
    assertThat(wallet.isActive()).isTrue();
  }

  /** SELECT ... FOR UPDATE is only legal inside a transaction, so the test needs one too. */
  @Test
  @Transactional
  void walletRowCanBeLockedForUpdate() {
    assertThat(walletRepository.findByIdForUpdate("wallet_1")).isPresent();
  }
}
