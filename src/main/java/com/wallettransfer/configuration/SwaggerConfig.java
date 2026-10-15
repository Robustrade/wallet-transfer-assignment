package com.wallettransfer.configuration;

import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.info.Contact;
import io.swagger.v3.oas.models.info.Info;
import io.swagger.v3.oas.models.servers.Server;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class SwaggerConfig {

  @Value("${server.servlet.context-path:/}")
  private String contextPath;

  @Bean
  public OpenAPI walletTransferOpenApi() {
    return new OpenAPI()
        .info(
            new Info()
                .title("Wallet Transfer Service")
                .description(
                    """
                    Wallet-to-wallet transfer APIs with idempotency, concurrency-safe balance updates, \
                    and double-entry ledger recording.

                    **Response codes (common):**
                    - 20001 — Transfer processed successfully
                    - 20002 — Wallet balance fetched successfully
                    - 20003 — Transfer history fetched successfully
                    - 5001 — Transfer failed (business rule)
                    - 5002 — Insufficient funds
                    - 5003 — Idempotency key conflict (same key, different payload)
                    - 5113 — Wallet not found
                    - 6017 — Invalid request / validation failure
                    - 5000 — Unexpected server error
                    """)
                .version("1.0.0")
                .contact(new Contact().name("Wallet Transfer Team")))
        .addServersItem(new Server().url(contextPath).description("Default server"));
  }
}
