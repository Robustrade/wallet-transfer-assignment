package com.api.wallet.domain;

public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED;

    public boolean canTransit(TransferStatus nextStatus){
        return switch (this) {
            case PENDING -> nextStatus == PROCESSED || nextStatus== FAILED;
            case FAILED,PROCESSED -> false;
        };
    }
}
