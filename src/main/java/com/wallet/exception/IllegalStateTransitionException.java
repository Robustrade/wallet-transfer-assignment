package com.wallet.exception;

import java.util.UUID;
import com.wallet.domain.TransferStatus;

public class IllegalStateTransitionException extends RuntimeException {
    public IllegalStateTransitionException(UUID transferId, TransferStatus from, TransferStatus to) {
        super("Transfer " + transferId + " cannot transition from " + from + " to " + to);
    }
}