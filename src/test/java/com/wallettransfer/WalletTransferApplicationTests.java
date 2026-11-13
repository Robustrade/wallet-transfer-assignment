package com.wallettransfer;

import static org.assertj.core.api.Assertions.assertThat;

import com.wallettransfer.support.PostgresIntegrationTest;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.ApplicationContext;

class WalletTransferApplicationTests extends PostgresIntegrationTest {

  @Autowired private ApplicationContext context;

  @Test
  void contextLoads() {
    assertThat(context.getId()).isNotBlank();
  }
}
