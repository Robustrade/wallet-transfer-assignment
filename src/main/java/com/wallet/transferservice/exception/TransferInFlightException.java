package com.wallet.transferservice.exception;

public class TransferInFlightException extends RuntimeException {
    public TransferInFlightException(String idempotencyKey) {
        super("A transfer with this idempotency key is already in-flight: " + idempotencyKey);
    }
}
