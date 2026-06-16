package com.viditec.wallettransfer.dto;

public record CreateWalletDto(
        String walletId,
        long initialBalance
) {
}
