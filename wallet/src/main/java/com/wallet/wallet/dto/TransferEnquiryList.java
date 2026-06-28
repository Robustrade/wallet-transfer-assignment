package com.wallet.wallet.dto;

import java.util.Date;

public record TransferEnquiryList(
        int toWalletId,
        double amount,
        Date date
) {
}
