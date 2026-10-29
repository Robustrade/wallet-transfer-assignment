package com.example.wallet.domain;

import java.util.UUID;

public class IllegalTransferStateException extends RuntimeException {
    public IllegalTransferStateException(UUID transferId, TransferStatus from, TransferStatus to) {
        super("Transfer " + transferId + " cannot move from " + from + " to " + to);
    }
}
