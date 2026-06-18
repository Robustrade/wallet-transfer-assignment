package com.wallet.transfer.exception;

/**
 * Base exception for wallet transfer service
 */
public class WalletTransferException extends RuntimeException {
    public WalletTransferException(String message) {
        super(message);
    }

    public WalletTransferException(String message, Throwable cause) {
        super(message, cause);
    }
}
