package com.wallet.transfer.exception;

/**
 * Thrown by domain model when a wallet operation would violate balance invariants
 * (e.g., debit exceeds balance, credit would overflow).
 * This is a defense-in-depth guard — the service layer should prevent these
 * conditions from reaching the domain model under normal operation.
 */
public class BalanceViolationException extends RuntimeException {

    private final String walletId;

    public BalanceViolationException(String walletId, String message) {
        super(message);
        this.walletId = walletId;
    }

    public String getWalletId() {
        return walletId;
    }
}
