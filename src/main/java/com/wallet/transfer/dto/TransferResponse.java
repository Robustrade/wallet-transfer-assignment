package com.wallet.transfer.dto;

import lombok.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Transfer Response DTO
 * 
 * Represents the HTTP response for a transfer operation.
 */
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class TransferResponse {
    private String transferId;
    private String fromWalletId;
    private String toWalletId;
    private BigDecimal amount;
    private String status;
    private LocalDateTime createdAt;
}
