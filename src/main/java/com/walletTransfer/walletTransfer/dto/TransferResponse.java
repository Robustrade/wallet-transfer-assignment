package com.walletTransfer.walletTransfer.dto;

import com.walletTransfer.walletTransfer.enums.TransferStatus;
import lombok.Builder;
import lombok.Getter;

import java.util.UUID;

@Getter
@Builder
public class TransferResponse {
    private UUID transferId;

    private TransferStatus status;

    private Long amount;
}
