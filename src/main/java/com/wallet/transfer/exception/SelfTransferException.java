package com.wallet.transfer.exception;

/**
 * Exception thrown when the sender tries to transfer money to the same wallet.
 */
public class SelfTransferException extends RuntimeException {

    /**
     * Constructs a new SelfTransferException with a detailed message.
     *
     * @param message Description of the self transfer error.
     */
    public SelfTransferException(String message) {
        super(message);
    }
}
