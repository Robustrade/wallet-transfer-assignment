package com.wallet.transfer.dto;

import com.wallet.transfer.domain.TransferState;
import java.time.LocalDateTime;

public record TransferResponse(
    String transferId,
    String fromWalletId,
    String toWalletId,
    Long amount,
    TransferState state,
    LocalDateTime createdAt
) {}
