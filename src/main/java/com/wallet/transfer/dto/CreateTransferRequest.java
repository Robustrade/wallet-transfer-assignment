package com.wallet.transfer.dto;

import lombok.*;
import java.math.BigDecimal;
import java.time.LocalDateTime;

/**
 * Create Transfer Request DTO
 * 
 * Represents the incoming HTTP request for creating a transfer.
 */
@Data
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class CreateTransferRequest {
    private String idempotencyKey;
    private String fromWalletId;
    private String toWalletId;
    private BigDecimal amount;
}
