package com.rajat.wallet;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.data.jpa.repository.config.EnableJpaAuditing;

@SpringBootApplication
@EnableJpaAuditing
public class WalletTransferApplication {

  public static void main(String[] args) {
    SpringApplication.run(WalletTransferApplication.class, args);
  }
}
