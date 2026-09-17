package com.robustrade.wallet.entities;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.enums.LedgerEntryType;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.test.util.ReflectionTestUtils;

class LedgerEntryTest {

  @Test
  void constructorPopulatesEveryFieldAndGeneratesAnId() {
    UUID transferId = UUID.randomUUID();
    UUID customerId = UUID.randomUUID();

    LedgerEntry entry =
        new LedgerEntry(
            transferId, customerId, "wallet_1", LedgerEntryType.DEBIT, new BigDecimal("100"));

    assertThat(entry.getId()).isNotNull();
    assertThat(entry.getTransferId()).isEqualTo(transferId);
    assertThat(entry.getCustomerId()).isEqualTo(customerId);
    assertThat(entry.getWalletId()).isEqualTo("wallet_1");
    assertThat(entry.getType()).isEqualTo(LedgerEntryType.DEBIT);
    assertThat(entry.getAmount()).isEqualByComparingTo("100");
    assertThat(entry.getCreatedAt()).isNull();
  }

  /**
   * Spring Data asks this to pick persist() over merge(). An entry is new until Hibernate stamps
   * createdAt on insert, which is what spares every save an extra SELECT.
   */
  @Test
  void isNewUntilItHasBeenStored() {
    LedgerEntry entry = entry();

    assertThat(entry.isNew()).isTrue();

    ReflectionTestUtils.setField(entry, "createdAt", Instant.parse("2025-01-01T00:00:00Z"));
    assertThat(entry.isNew()).isFalse();
  }

  private static LedgerEntry entry() {
    return new LedgerEntry(
        UUID.randomUUID(), UUID.randomUUID(), "wallet_1", LedgerEntryType.CREDIT, BigDecimal.ONE);
  }
}
