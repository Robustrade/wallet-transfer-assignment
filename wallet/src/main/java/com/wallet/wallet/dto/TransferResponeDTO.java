package com.wallet.wallet.dto;

import java.util.Date;

public record TransferResponeDTO(
        int toWalletId,
        double amount,
        int transactionId,
        Date date
) {
}
