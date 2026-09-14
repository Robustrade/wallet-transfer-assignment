package com.robustrade.wallet.handler.dto;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class StatementResponse {

    private String walletId;
    private Long userId;
    private BigDecimal currentBalance;

    @Builder.Default
    private List<StatementEntryResponse> entries = new ArrayList<>();
}
