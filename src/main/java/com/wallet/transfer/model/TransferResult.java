package com.wallet.transfer.model;

import lombok.AllArgsConstructor;
import lombok.Getter;

/**
 * Immutable value object representing the result of a transfer processing attempt.
 * Contains the status code and response body that should be returned to the client.
 */
@Getter
@AllArgsConstructor
public class TransferResult {

    /**
     * The HTTP status code to return.
     */
    private final int statusCode;

    /**
     * The serialized JSON response body payload.
     */
    private final String responseBody;
}
