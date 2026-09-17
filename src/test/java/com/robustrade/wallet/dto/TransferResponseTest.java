package com.robustrade.wallet.dto;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.entities.Transfer;
import com.robustrade.wallet.enums.FailureReason;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class TransferResponseTest {

  @Test
  void copiesEveryFieldOffTheTransfer() {
    Transfer transfer = pending();
    transfer.markProcessed();

    TransferResponse response = TransferResponse.from(transfer);

    assertThat(response.transferId()).isEqualTo(transfer.getId());
    assertThat(response.status()).isEqualTo("PROCESSED");
    assertThat(response.fromWalletId()).isEqualTo("wallet_1");
    assertThat(response.toWalletId()).isEqualTo("wallet_2");
    assertThat(response.amount()).isEqualByComparingTo("100");
    assertThat(response.failureReason()).isNull();
    assertThat(response.createdAt()).isEqualTo(transfer.getCreatedAt());
  }

  @Test
  void rendersTheFailureReasonByName() {
    Transfer transfer = pending();
    transfer.markFailed(FailureReason.WALLET_NOT_ACTIVE);

    TransferResponse response = TransferResponse.from(transfer);

    assertThat(response.status()).isEqualTo("FAILED");
    assertThat(response.failureReason()).isEqualTo("WALLET_NOT_ACTIVE");
  }

  @Test
  void aPendingTransferHasNoFailureReason() {
    assertThat(TransferResponse.from(pending()).failureReason()).isNull();
  }

  private static Transfer pending() {
    return new Transfer("key-1", "wallet_1", "wallet_2", UUID.randomUUID(), new BigDecimal("100"));
  }
}
