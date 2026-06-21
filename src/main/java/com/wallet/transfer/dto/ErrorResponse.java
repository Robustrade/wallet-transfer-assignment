package com.wallet.transfer.dto;

import java.time.Instant;

public record ErrorResponse(
        String error,
        String message,
        Instant timestamp
) {
    public static ErrorResponse of(String error, String message) {
        return new ErrorResponse(error, message, Instant.now());
    }
}
