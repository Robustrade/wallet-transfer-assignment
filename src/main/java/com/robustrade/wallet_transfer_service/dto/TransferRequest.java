package com.robustrade.wallet_transfer_service.dto;

import lombok.Data;

@Data
public class TransferRequest {

    private String idempotencyKey;
    private Long fromWalletId;
    private Long toWalletId;
    private Long amount;
}