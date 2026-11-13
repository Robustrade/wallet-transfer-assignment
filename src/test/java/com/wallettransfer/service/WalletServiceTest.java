package com.wallettransfer.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.domain.ValidationException;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletAlreadyExistsException;
import com.wallettransfer.domain.WalletNotFoundException;
import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

class WalletServiceTest extends PostgresIntegrationTest {

  @Autowired private WalletService service;

  @Test
  void createsAWalletAndReadsItBack() {
    Wallet created = service.create("wallet_1", 1_000);

    Wallet found = service.get("wallet_1");

    assertThat(found.id()).isEqualTo(created.id());
    assertThat(found.balance()).isEqualTo(1_000);
    assertThat(found.openingBalance()).isEqualTo(1_000);
  }

  @Test
  void validatesInput() {
    assertThatThrownBy(() -> service.create("", 0)).isInstanceOf(ValidationException.class);
    assertThatThrownBy(() -> service.create("w", -1)).isInstanceOf(ValidationException.class);
  }

  @Test
  void rejectsDuplicates() {
    service.create("w", 5);

    assertThatThrownBy(() -> service.create("w", 5))
        .isInstanceOf(WalletAlreadyExistsException.class);
  }

  @Test
  void unknownWalletIsNotFound() {
    assertThatThrownBy(() -> service.get("nope")).isInstanceOf(WalletNotFoundException.class);
  }
}
