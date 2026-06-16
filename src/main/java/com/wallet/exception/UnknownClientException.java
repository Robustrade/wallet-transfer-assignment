package com.wallet.exception;

/**
 * Thrown when an incoming request carries an X-Client-Id that is not present
 * in the {@code clients} registry. Mapped to HTTP 403 by the global handler.
 */
public class UnknownClientException extends RuntimeException {

    private final String clientId;

    public UnknownClientException(String clientId) {
        super("Unknown client: '" + clientId + "'. Client is not registered.");
        this.clientId = clientId;
    }

    public String getClientId() {
        return clientId;
    }
}
