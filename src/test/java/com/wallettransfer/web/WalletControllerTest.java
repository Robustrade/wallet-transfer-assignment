package com.wallettransfer.web;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.ResultActions;

@AutoConfigureMockMvc
class WalletControllerTest extends PostgresIntegrationTest {

  private static final String WALLET = "{\"id\":\"w1\",\"openingBalance\":100}";

  @Autowired private MockMvc mvc;

  private ResultActions postWallet(String body) throws Exception {
    return mvc.perform(post("/wallets").contentType(MediaType.APPLICATION_JSON).content(body));
  }

  @Test
  void createsAWallet() throws Exception {
    postWallet(WALLET)
        .andExpect(status().isCreated())
        .andExpect(jsonPath("$.id").value("w1"))
        .andExpect(jsonPath("$.balance").value(100))
        .andExpect(jsonPath("$.openingBalance").value(100));
  }

  @Test
  void aSecondWalletWithTheSameIdIs409() throws Exception {
    postWallet(WALLET).andExpect(status().isCreated());

    postWallet(WALLET)
        .andExpect(status().isConflict())
        .andExpect(jsonPath("$.error.code").value("WALLET_ALREADY_EXISTS"));
  }

  @Test
  void readsTheBalanceOfAWallet() throws Exception {
    postWallet(WALLET);

    mvc.perform(get("/wallets/{id}", "w1"))
        .andExpect(status().isOk())
        .andExpect(jsonPath("$.id").value("w1"))
        .andExpect(jsonPath("$.balance").value(100));
  }

  @Test
  void anUnknownWalletIs404() throws Exception {
    mvc.perform(get("/wallets/{id}", "ghost"))
        .andExpect(status().isNotFound())
        .andExpect(jsonPath("$.error.code").value("WALLET_NOT_FOUND"));
  }

  @ParameterizedTest(name = "{0}")
  @CsvSource(
      delimiter = '|',
      value = {
        "malformed json|{\"id\":",
        "missing id|{\"openingBalance\":1}",
        "blank id|{\"id\":\" \",\"openingBalance\":1}",
        "missing balance|{\"id\":\"w1\"}",
        "negative balance|{\"id\":\"w1\",\"openingBalance\":-1}",
        "fractional balance|{\"id\":\"w1\",\"openingBalance\":1.5}",
        "unknown field|{\"id\":\"w1\",\"openingBalance\":1,\"bogus\":1}"
      })
  void rejectsBadRequestsWithA400(String name, String body) throws Exception {
    postWallet(body)
        .andExpect(status().isBadRequest())
        .andExpect(jsonPath("$.error.code").isNotEmpty());
  }
}
