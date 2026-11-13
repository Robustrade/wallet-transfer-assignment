package com.wallettransfer.web;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.header;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.jayway.jsonpath.JsonPath;
import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.support.LedgerInvariants;
import com.wallettransfer.support.PostgresIntegrationTest;
import java.time.Instant;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;

@AutoConfigureMockMvc
class TransferControllerTest extends PostgresIntegrationTest {

  private static final String TRANSFER =
      """
      {"idempotencyKey":"abc123","fromWalletId":"w1","toWalletId":"w2","amount":30}""";

  @Autowired private MockMvc mvc;
  @Autowired private WalletRepository wallets;

  @BeforeEach
  void seedWallets() {
    Instant now = Instant.parse("2026-01-02T03:04:05Z");
    wallets.create(Wallet.open("w1", 100, now));
    wallets.create(Wallet.open("w2", 50, now));
  }

  private ResultActions postTransfer(String body) throws Exception {
    return mvc.perform(post("/transfers").contentType(MediaType.APPLICATION_JSON).content(body));
  }

  @Test
  void createsATransfer() throws Exception {
    postTransfer(TRANSFER)
        .andExpect(status().isCreated())
        .andExpect(header().doesNotExist("Idempotent-Replayed"))
        .andExpect(jsonPath("$.id").isNotEmpty())
        .andExpect(jsonPath("$.state").value("PROCESSED"))
        .andExpect(jsonPath("$.idempotencyKey").value("abc123"))
        .andExpect(jsonPath("$.fromWalletId").value("w1"))
        .andExpect(jsonPath("$.toWalletId").value("w2"))
        .andExpect(jsonPath("$.amount").value(30))
        .andExpect(jsonPath("$.failureReason").doesNotExist());

    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void aDuplicateRequestReturnsTheOriginalResult() throws Exception {
    String firstBody = postTransfer(TRANSFER).andReturn().getResponse().getContentAsString();

    String replayBody =
        postTransfer(TRANSFER)
            .andExpect(status().isOk())
            .andExpect(header().string("Idempotent-Replayed", "true"))
            .andReturn()
            .getResponse()
            .getContentAsString();

    assertThat(replayBody).isEqualTo(firstBody);
    LedgerInvariants.assertHold(jdbc);
  }

  @Test
  void insufficientFundsIs422WithAFailedTransferAndIsReplayedIdentically() throws Exception {
    String body =
        """
        {"idempotencyKey":"k","fromWalletId":"w1","toWalletId":"w2","amount":1000}""";

    String first =
        postTransfer(body)
            .andExpect(status().isUnprocessableEntity())
            .andExpect(jsonPath("$.state").value("FAILED"))
            .andExpect(jsonPath("$.failureReason").value("INSUFFICIENT_FUNDS"))
            .andReturn()
            .getResponse()
            .getContentAsString();

    String replay =
        postTransfer(body)
            .andExpect(status().isUnprocessableEntity())
            .andExpect(header().string("Idempotent-Replayed", "true"))
            .andReturn()
            .getResponse()
            .getContentAsString();
    assertThat(replay).isEqualTo(first);
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource(
      delimiter = '|',
      value = {
        "malformed json|{\"fromWalletId\":",
        "empty body|",
        "unknown field|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":1,\"bogus\":true}",
        "amount as a string|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":\"5\"}",
        "fractional amount|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":1.5}",
        "zero amount|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":0}",
        "negative amount|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":-5}",
        "missing amount|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\"}",
        "missing source|{\"toWalletId\":\"w2\",\"amount\":1}",
        "same wallet|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w1\",\"amount\":1}",
        "trailing json|{\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\",\"amount\":1}{}"
      })
  void rejectsBadRequestsWithA400(String name, String body) throws Exception {
    postTransfer(body == null ? "" : body)
        .andExpect(status().isBadRequest())
        .andExpect(jsonPath("$.error.code").isNotEmpty())
        .andExpect(jsonPath("$.error.message").isNotEmpty());
  }

  @Test
  void unknownWalletIs404() throws Exception {
    postTransfer("{\"fromWalletId\":\"ghost\",\"toWalletId\":\"w2\",\"amount\":1}")
        .andExpect(status().isNotFound())
        .andExpect(jsonPath("$.error.code").value("WALLET_NOT_FOUND"));
  }

  @Test
  void reusingAKeyWithADifferentPayloadIs409() throws Exception {
    postTransfer(TRANSFER).andExpect(status().isCreated());

    postTransfer(
            "{\"idempotencyKey\":\"abc123\",\"fromWalletId\":\"w1\",\"toWalletId\":\"w2\","
                + "\"amount\":31}")
        .andExpect(status().isConflict())
        .andExpect(jsonPath("$.error.code").value("IDEMPOTENCY_KEY_REUSED"));
  }

  @Test
  void fetchesATransferById() throws Exception {
    String created = postTransfer(TRANSFER).andReturn().getResponse().getContentAsString();
    String id = JsonPath.read(created, "$.id");

    String fetched =
        mvc.perform(get("/transfers/{id}", id))
            .andExpect(status().isOk())
            .andExpect(header().doesNotExist("Idempotent-Replayed"))
            .andReturn()
            .getResponse()
            .getContentAsString();

    assertThat(fetched).isEqualTo(created);
  }

  @Test
  void fetchingAnUnknownTransferIs404() throws Exception {
    mvc.perform(get("/transfers/{id}", "nope"))
        .andExpect(status().isNotFound())
        .andExpect(jsonPath("$.error.code").value("TRANSFER_NOT_FOUND"));
  }
}
