package com.example.wallet_transfer_assignment.dto;



import com.example.wallet_transfer_assignment.entity.TransferState;

import java.math.BigDecimal;
import java.util.UUID;

public record TransferResponse(

        UUID transferId,

        UUID fromWalletId,

        UUID toWalletId,

        BigDecimal amount,

        TransferState state

) {
}