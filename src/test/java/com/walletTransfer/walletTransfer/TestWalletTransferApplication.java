package com.walletTransfer.walletTransfer;

import org.springframework.boot.SpringApplication;

public class TestWalletTransferApplication {

	public static void main(String[] args) {
		SpringApplication.from(WalletTransferApplication::main).with(TestcontainersConfiguration.class).run(args);
	}

}
