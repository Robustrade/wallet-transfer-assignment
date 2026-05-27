package com.wallettransfer.service.exception;

/**
 * Thrown when transfer or wallet input violates business rules.
 */
public class InvalidRequestException extends RuntimeException {

    /**
     * @param message detail for API error response
     */
    public InvalidRequestException(String message) {
        super(message);
    }
}
