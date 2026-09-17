package com.robustrade.wallet.dto;

import static org.assertj.core.api.Assertions.assertThat;

import jakarta.validation.Validation;
import jakarta.validation.Validator;
import jakarta.validation.ValidatorFactory;
import java.math.BigDecimal;
import java.util.Set;
import java.util.stream.Collectors;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

class CreateTransferRequestTest {

  private static ValidatorFactory factory;
  private static Validator validator;

  @BeforeAll
  static void setUp() {
    factory = Validation.buildDefaultValidatorFactory();
    validator = factory.getValidator();
  }

  @AfterAll
  static void tearDown() {
    factory.close();
  }

  @Test
  void aWellFormedRequestPasses() {
    assertThat(validator.validate(request("key-1", "wallet_1", "wallet_2", "100"))).isEmpty();
  }

  @Test
  void blankFieldsAreRejected() {
    assertThat(violatedFields(request("", "wallet_1", "wallet_2", "100")))
        .contains("idempotencyKey");
    assertThat(violatedFields(request("key-1", " ", "wallet_2", "100"))).contains("fromWalletId");
    assertThat(violatedFields(request("key-1", "wallet_1", "", "100"))).contains("toWalletId");
  }

  @Test
  void anIdempotencyKeyLongerThanTheColumnIsRejected() {
    assertThat(violatedFields(request("k".repeat(256), "wallet_1", "wallet_2", "100")))
        .contains("idempotencyKey");
    assertThat(violatedFields(request("k".repeat(255), "wallet_1", "wallet_2", "100"))).isEmpty();
  }

  @Test
  void nonPositiveAndMissingAmountsAreRejected() {
    assertThat(violatedFields(request("key-1", "wallet_1", "wallet_2", "0"))).contains("amount");
    assertThat(violatedFields(request("key-1", "wallet_1", "wallet_2", "-1"))).contains("amount");
    assertThat(violatedFields(new CreateTransferRequest("key-1", "wallet_1", "wallet_2", null)))
        .contains("amount");
  }

  /** Caught here as a 400 rather than letting the DB CHECK constraint surface as a 500. */
  @Test
  void aTransferToTheSameWalletIsRejected() {
    assertThat(violatedFields(request("key-1", "wallet_1", "wallet_1", "100")))
        .contains("toWalletIdDifferent");
  }

  @Test
  void theSameWalletCheckReadsDirectly() {
    assertThat(request("key-1", "wallet_1", "wallet_2", "100").isToWalletIdDifferent()).isTrue();
    assertThat(request("key-1", "wallet_1", "wallet_1", "100").isToWalletIdDifferent()).isFalse();
    // A null source is @NotBlank's problem to report, not this check's.
    assertThat(
            new CreateTransferRequest("key-1", null, "wallet_1", BigDecimal.ONE)
                .isToWalletIdDifferent())
        .isTrue();
  }

  private static Set<String> violatedFields(CreateTransferRequest request) {
    return validator.validate(request).stream()
        .map(violation -> violation.getPropertyPath().toString())
        .collect(Collectors.toSet());
  }

  private static CreateTransferRequest request(
      String key, String fromId, String toId, String amount) {
    return new CreateTransferRequest(key, fromId, toId, new BigDecimal(amount));
  }
}
