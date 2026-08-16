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

    /** HTTP status a finished transfer of this state is reported with. */
    public int httpStatus() {
        return switch (this) {
            case PROCESSED -> 201;
            case FAILED -> 400;
            case PENDING -> throw new IllegalStateException("PENDING transfers are never returned to callers");
        };
    }
}
