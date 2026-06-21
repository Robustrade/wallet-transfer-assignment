package com.mycompany.wallet_transaction.dto;

import com.mycompany.wallet_transaction.entities.enums.TransferStatus;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.math.BigDecimal;

@Data
@AllArgsConstructor
@NoArgsConstructor
@Builder
public class PaymentResponseDto {

    private Long transferId;

    private Long fromWalletId;

    private Long toWalletId;

    private BigDecimal amount;

    private TransferStatus status;

    private String message;
}