package com.wallet.transfer.exception;

/**
 * Exception thrown when the source wallet does not contain enough balance
 * to complete the requested transfer.
 */
public class InsufficientFundsException extends RuntimeException {

    /**
     * Constructs a new InsufficientFundsException with a detailed message.
     *
     * @param message Description of the insufficient funds error.
     */
    public InsufficientFundsException(String message) {
        super(message);
    }
}
