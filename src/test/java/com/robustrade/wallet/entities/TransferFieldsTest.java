package com.robustrade.wallet.entities;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.enums.TransferStatus;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

/** Construction and accessors. State transitions are covered by {@link TransferStateTest}. */
class TransferFieldsTest {

  @Test
  void constructorPopulatesEveryFieldAndGeneratesAnId() {
    UUID initiatedBy = UUID.randomUUID();
    Transfer transfer =
        new Transfer("key-1", "wallet_1", "wallet_2", initiatedBy, new BigDecimal("100"));

    assertThat(transfer.getId()).isNotNull();
    assertThat(transfer.getIdempotencyKey()).isEqualTo("key-1");
    assertThat(transfer.getFromWalletId()).isEqualTo("wallet_1");
    assertThat(transfer.getToWalletId()).isEqualTo("wallet_2");
    assertThat(transfer.getInitiatedBy()).isEqualTo(initiatedBy);
    assertThat(transfer.getAmount()).isEqualByComparingTo("100");
    assertThat(transfer.getStatus()).isEqualTo(TransferStatus.PENDING);
    assertThat(transfer.getFailureReason()).isNull();
  }

  /** The timestamps are written by Hibernate, so they are null until the row is persisted. */
  @Test
  void timestampsAreUnsetBeforePersisting() {
    Transfer transfer = pending();

    assertThat(transfer.getCreatedAt()).isNull();
    assertThat(transfer.getUpdatedAt()).isNull();
  }

  @Test
  void matchesRequiresEveryFieldToAgree() {
    Transfer transfer = pending();

    assertThat(transfer.matches("wallet_1", "wallet_2", new BigDecimal("100"))).isTrue();
    assertThat(transfer.matches("wallet_9", "wallet_2", new BigDecimal("100"))).isFalse();
    assertThat(transfer.matches("wallet_1", "wallet_9", new BigDecimal("100"))).isFalse();
    assertThat(transfer.matches("wallet_1", "wallet_2", new BigDecimal("101"))).isFalse();
  }

  private static Transfer pending() {
    return new Transfer("key-1", "wallet_1", "wallet_2", UUID.randomUUID(), new BigDecimal("100"));
  }
}
