package com.robustrade.wallet.entities;

import com.robustrade.wallet.enums.WalletStatus;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import org.springframework.test.util.ReflectionTestUtils;

/**
 * Builds {@link Wallet} instances for tests. Wallet has no public constructor because JPA owns its
 * lifecycle, so the fields are set reflectively rather than adding setters that production code
 * would never use.
 */
public final class Wallets {

  public static final Instant CREATED_AT = Instant.parse("2025-01-01T00:00:00Z");
  public static final Instant UPDATED_AT = Instant.parse("2025-01-02T00:00:00Z");

  private Wallets() {}

  public static Wallet active(String id, String balance) {
    return wallet(id, UUID.randomUUID(), WalletStatus.ACTIVE, new BigDecimal(balance));
  }

  public static Wallet with(String id, WalletStatus status, String balance) {
    return wallet(id, UUID.randomUUID(), status, new BigDecimal(balance));
  }

  public static Wallet wallet(String id, UUID customerId, WalletStatus status, BigDecimal balance) {
    Wallet wallet = new Wallet();
    ReflectionTestUtils.setField(wallet, "id", id);
    ReflectionTestUtils.setField(wallet, "customerId", customerId);
    ReflectionTestUtils.setField(wallet, "name", id + " name");
    ReflectionTestUtils.setField(wallet, "status", status);
    ReflectionTestUtils.setField(wallet, "balance", balance);
    ReflectionTestUtils.setField(wallet, "meta", "{}");
    ReflectionTestUtils.setField(wallet, "createdAt", CREATED_AT);
    ReflectionTestUtils.setField(wallet, "updatedAt", UPDATED_AT);
    return wallet;
  }
}
