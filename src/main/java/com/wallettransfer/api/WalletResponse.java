package com.wallettransfer.api;

import java.math.BigDecimal;

public record WalletResponse(String walletId, BigDecimal balance) {
}