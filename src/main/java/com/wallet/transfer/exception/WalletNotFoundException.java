package com.wallet.transfer.exception;

/**
 * Thrown when a wallet is not found
 */
public class WalletNotFoundException extends WalletTransferException {
    public WalletNotFoundException(String walletId) {
        super("Wallet not found: " + walletId);
    }
}
