package com.robustrade.wallet.service;

public class TransferServiceException extends ServiceException {

    public TransferServiceException(String errorCode, String message, int httpStatus) {
        super(errorCode, message, httpStatus);
    }

    public static TransferServiceException walletsNotFound() {
        return new TransferServiceException("WALLETS_NOT_FOUND", "Wallets not found", 404);
    }

    public static TransferServiceException idempotencyConflict() {
        return new TransferServiceException(
                "IDEMPOTENCY_CONFLICT",
                "Idempotency key already used with a different request payload",
                409
        );
    }

    public static TransferServiceException insufficientBalance(String reason) {
        return new TransferServiceException("INSUFFICIENT_BALANCE", reason, 400);
    }

    public static TransferServiceException transferFailed(String reason) {
        return new TransferServiceException("TRANSFER_FAILED", reason, 500);
    }
}
