package com.walletTransfer.walletTransfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.retry.annotation.EnableRetry;

@SpringBootApplication
@EnableRetry
public class WalletTransferApplication {

	public static void main(String[] args) {
		SpringApplication.run(WalletTransferApplication.class, args);
	}

}
