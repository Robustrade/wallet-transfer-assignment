package com.robustrade.wallet.entities;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.LedgerEntryType;
import com.robustrade.wallet.enums.TransferStatus;
import com.robustrade.wallet.enums.WalletStatus;
import org.junit.jupiter.api.Test;

/**
 * The enum constants are persisted by name (@Enumerated(STRING)), so renaming one silently breaks
 * every stored row. These assertions pin the wire format.
 */
class DomainEnumsTest {

  @Test
  void transferStatusNamesAreStable() {
    assertThat(TransferStatus.values())
        .containsExactly(TransferStatus.PENDING, TransferStatus.PROCESSED, TransferStatus.FAILED);
    assertThat(TransferStatus.valueOf("PENDING")).isEqualTo(TransferStatus.PENDING);
  }

  @Test
  void walletStatusNamesAreStable() {
    assertThat(WalletStatus.values())
        .containsExactly(WalletStatus.ACTIVE, WalletStatus.INACTIVE, WalletStatus.BLOCKED);
    assertThat(WalletStatus.valueOf("ACTIVE")).isEqualTo(WalletStatus.ACTIVE);
  }

  @Test
  void failureReasonNamesAreStable() {
    assertThat(FailureReason.values())
        .containsExactly(FailureReason.INSUFFICIENT_FUNDS, FailureReason.WALLET_NOT_ACTIVE);
    assertThat(FailureReason.valueOf("INSUFFICIENT_FUNDS"))
        .isEqualTo(FailureReason.INSUFFICIENT_FUNDS);
  }

  @Test
  void ledgerEntryTypeNamesAreStable() {
    assertThat(LedgerEntryType.values())
        .containsExactly(LedgerEntryType.DEBIT, LedgerEntryType.CREDIT);
    assertThat(LedgerEntryType.valueOf("DEBIT")).isEqualTo(LedgerEntryType.DEBIT);
  }
}
