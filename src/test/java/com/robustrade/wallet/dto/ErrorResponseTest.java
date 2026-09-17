package com.robustrade.wallet.dto;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Map;
import org.junit.jupiter.api.Test;

class ErrorResponseTest {

  @Test
  void theShorthandLeavesDetailsOut() {
    ErrorResponse response = ErrorResponse.of("WALLET_NOT_FOUND", "Wallet not found: wallet_9");

    assertThat(response.error()).isEqualTo("WALLET_NOT_FOUND");
    assertThat(response.message()).isEqualTo("Wallet not found: wallet_9");
    assertThat(response.details()).isNull();
  }

  @Test
  void detailsAreKeptWhenSupplied() {
    Map<String, String> details = Map.of("amount", "must be greater than 0");

    ErrorResponse response = new ErrorResponse("VALIDATION_FAILED", "Request is invalid", details);

    assertThat(response.details()).isEqualTo(details);
  }
}
