package com.siddharth.wallet.dto;

import com.siddharth.wallet.model.TransferStatus;

import java.math.BigDecimal;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        TransferStatus status,
        UUID fromWalletId,
        UUID toWalletId,
        BigDecimal amount
) {}