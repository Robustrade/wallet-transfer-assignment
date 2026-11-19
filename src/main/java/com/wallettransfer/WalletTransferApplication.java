package com.wallettransfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/** Entry point of the wallet transfer service. */
@SpringBootApplication
public class WalletTransferApplication {

  public static void main(String[] args) {
    SpringApplication.run(WalletTransferApplication.class, args);
  }
}
