package com.wallet.transfer.exception;

/**
 * Exception thrown when a wallet ID specified in the transfer transaction
 * cannot be found in the database.
 */
public class WalletNotFoundException extends RuntimeException {

    /**
     * Constructs a new WalletNotFoundException with a detailed message.
     *
     * @param message Description of the missing wallet error.
     */
    public WalletNotFoundException(String message) {
        super(message);
    }
}
