package com.example.wallet.web.dto;

import com.example.wallet.domain.TransferStatus;
import java.math.BigDecimal;

public record TransferResponse(
        String transferId,
        String fromWalletId,
        String toWalletId,
        BigDecimal amount,
        TransferStatus status,
        String errorMessage) {}
