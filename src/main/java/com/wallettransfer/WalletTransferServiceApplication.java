package com.wallettransfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.autoconfigure.domain.EntityScan;
import org.springframework.context.annotation.ComponentScan;

@ComponentScan(basePackages = {"com.wallettransfer.*"})
@EntityScan("com.wallettransfer.*")
@SpringBootApplication
public class WalletTransferServiceApplication {

  public static void main(String[] args) {
    SpringApplication.run(WalletTransferServiceApplication.class, args);
  }
}
