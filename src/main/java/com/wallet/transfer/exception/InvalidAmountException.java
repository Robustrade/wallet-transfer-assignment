package com.wallet.transfer.exception;

/**
 * Exception thrown when the transfer amount is invalid (e.g., zero or negative).
 */
public class InvalidAmountException extends RuntimeException {

    /**
     * Constructs a new InvalidAmountException with a detailed message.
     *
     * @param message Description of the invalid amount error.
     */
    public InvalidAmountException(String message) {
        super(message);
    }
}
