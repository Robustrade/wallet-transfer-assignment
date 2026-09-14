package com.robustrade.wallet.handler.dto;

import com.robustrade.wallet.domain.TransactionType;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class StatementEntryResponse {

    private UUID transferId;
    private TransactionType transferType;
    private BigDecimal amount;
    private BigDecimal previousBalance;
    private BigDecimal balanceAfterTransfer;
    private Instant transferDate;
}
