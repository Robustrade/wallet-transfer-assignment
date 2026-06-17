package com.siddharth.wallet.exceptions;

import java.util.UUID;

public class InsufficientFundsException
        extends RuntimeException {

    public InsufficientFundsException(UUID walletId) {
        super("Insufficient funds for wallet: " + walletId);
    }
}
