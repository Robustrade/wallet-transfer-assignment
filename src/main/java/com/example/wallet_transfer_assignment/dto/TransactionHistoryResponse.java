package com.example.wallet_transfer_assignment.dto;

import com.example.wallet_transfer_assignment.entity.EntryType;

import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

public record TransactionHistoryResponse(

        UUID transferId,

        EntryType entryType,

        BigDecimal amount,

        UUID counterPartyWalletId,

        LocalDateTime transactionTime
) {
}