package com.wallet.dto;

public record TransferExecutionResult(TransferResponse transfer, boolean isNewTransfer) {

    public static TransferExecutionResult newTransfer(TransferResponse response) {
        return new TransferExecutionResult(response, true);
    }

    public static TransferExecutionResult duplicate(TransferResponse response) {
        return new TransferExecutionResult(response, false);
    }
}
