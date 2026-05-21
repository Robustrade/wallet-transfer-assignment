package com.kullu.wallet.exception;

import lombok.Getter;

@Getter
public class WalletNotFoundException extends RuntimeException {

    private final String walletId;

    public WalletNotFoundException(final String walletId) {
        super("wallet not found: " + walletId);
        this.walletId = walletId;
    }
}
