package com.videtech.wallet_transfer.dto;


import lombok.AllArgsConstructor;
import lombok.Getter;
import java.math.BigDecimal;
import java.time.LocalDateTime;

@Getter
@AllArgsConstructor
public class TransferResponse {

    private String transferId;
    private String status;
    private String fromWalletId;
    private String toWalletId;
    private BigDecimal amount;
    private LocalDateTime createdAt;
}