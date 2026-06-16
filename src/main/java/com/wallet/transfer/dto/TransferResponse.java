package com.wallet.transfer.dto;

import com.wallet.transfer.entity.TransferState;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.math.BigDecimal;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class TransferResponse {
    private String transferId;
    private String fromWalletId;
    private String toWalletId;
    private BigDecimal amount;
    private TransferState state;
    private String message;
}
