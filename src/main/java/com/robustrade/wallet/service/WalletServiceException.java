package com.robustrade.wallet.service;

public class WalletServiceException extends ServiceException {

    public WalletServiceException(String errorCode, String message, int httpStatus) {
        super(errorCode, message, httpStatus);
    }

    public static WalletServiceException walletAlreadyExists(Long userId) {
        return new WalletServiceException(
                "WALLET_ALREADY_EXISTS",
                "Wallet already exists for userId: " + userId,
                409
        );
    }

    public static WalletServiceException walletNotFound() {
        return new WalletServiceException("WALLET_NOT_FOUND", "Wallet not found", 404);
    }

    public static WalletServiceException multipleWalletsForUser() {
        return new WalletServiceException(
                "MULTIPLE_WALLETS",
                "Multiple wallets found for userId; please provide walletId",
                400
        );
    }

    public static WalletServiceException walletUserMismatch() {
        return new WalletServiceException(
                "WALLET_USER_MISMATCH",
                "walletId does not belong to the given userId",
                400
        );
    }

    public static WalletServiceException missingLookup() {
        return new WalletServiceException(
                "VALIDATION_ERROR",
                "Either userId or walletId is mandatory",
                400
        );
    }
}
