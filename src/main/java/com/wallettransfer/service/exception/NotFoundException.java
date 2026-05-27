package com.wallettransfer.service.exception;

/**
 * Thrown when a requested wallet or transfer does not exist.
 */
public class NotFoundException extends RuntimeException {

    /**
     * @param message detail for API error response
     */
    public NotFoundException(String message) {
        super(message);
    }
}
