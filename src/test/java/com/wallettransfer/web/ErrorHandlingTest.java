package com.wallettransfer.web;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.wallettransfer.service.CreateTransferCommand;
import com.wallettransfer.service.TransferService;
import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.http.MediaType;
import org.springframework.test.context.bean.override.mockito.MockitoBean;
import org.springframework.test.web.servlet.MockMvc;

@AutoConfigureMockMvc
class ErrorHandlingTest extends PostgresIntegrationTest {

  @Autowired private MockMvc mvc;
  @MockitoBean private TransferService transferService;

  @Test
  void anUnexpectedFailureIsAnOpaque500() throws Exception {
    when(transferService.createTransfer(any(CreateTransferCommand.class)))
        .thenThrow(new IllegalStateException("jdbc:postgresql://secret-host/pw=hunter2"));

    String body =
        mvc.perform(
                post("/transfers")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}"))
            .andExpect(status().isInternalServerError())
            .andExpect(jsonPath("$.error.code").value("INTERNAL_ERROR"))
            .andReturn()
            .getResponse()
            .getContentAsString();

    assertThat(body).doesNotContain("secret-host").doesNotContain("hunter2");
  }

  @Test
  void aNonJsonContentTypeIs415() throws Exception {
    mvc.perform(post("/transfers").contentType(MediaType.TEXT_PLAIN).content("hi"))
        .andExpect(status().isUnsupportedMediaType())
        .andExpect(jsonPath("$.error.code").value("HTTP_415"));
  }

  @Test
  void anUnsupportedMethodIs405() throws Exception {
    mvc.perform(delete("/transfers/abc"))
        .andExpect(status().isMethodNotAllowed())
        .andExpect(jsonPath("$.error.code").value("HTTP_405"));
  }
}
