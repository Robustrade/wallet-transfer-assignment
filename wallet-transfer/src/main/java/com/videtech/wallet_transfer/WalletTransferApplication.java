package com.videtech.wallet_transfer;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication
public class WalletTransferApplication {

	public static void main(String[] args) {
		

		java.util.TimeZone.setDefault(java.util.TimeZone.getTimeZone("UTC"));

		
		SpringApplication.run(WalletTransferApplication.class, args);
		
	}

}
