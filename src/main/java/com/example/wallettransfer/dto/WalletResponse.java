package com.example.wallettransfer.dto;

import java.math.BigDecimal;

public record WalletResponse(String walletId, BigDecimal balance) {
}
