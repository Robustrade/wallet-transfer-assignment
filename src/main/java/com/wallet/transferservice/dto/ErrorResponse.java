package com.wallet.transferservice.dto;

/**
 * Standard error response body for all API errors.
 */
public record ErrorResponse(
        String code,
        String message,
        String transferId
) {
    public ErrorResponse(String code, String message) {
        this(code, message, null);
    }
}
