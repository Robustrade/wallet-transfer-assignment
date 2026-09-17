package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.entities.Transfer;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class TransferResultTest {

  private static final Transfer TRANSFER =
      new Transfer("key-1", "wallet_1", "wallet_2", UUID.randomUUID(), new BigDecimal("100"));

  @Test
  void executedIsNotAReplay() {
    TransferResult result = TransferResult.executed(TRANSFER);

    assertThat(result.transfer()).isSameAs(TRANSFER);
    assertThat(result.replayed()).isFalse();
  }

  @Test
  void replayedIsAReplay() {
    TransferResult result = TransferResult.replayed(TRANSFER);

    assertThat(result.transfer()).isSameAs(TRANSFER);
    assertThat(result.replayed()).isTrue();
  }
}
