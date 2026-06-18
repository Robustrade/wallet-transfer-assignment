package com.wallet.handler;

import java.util.UUID;

import com.wallet.exception.InvalidTransferRequestException;

/** Parses path/body id values shared by every controller in this package. */
final class RequestIds {

    private RequestIds() {
    }

    static UUID parseUuid(String value) {
        try {
            return UUID.fromString(value);
        } catch (IllegalArgumentException e) {
            throw new InvalidTransferRequestException("Invalid id: " + value);
        }
    }
}
