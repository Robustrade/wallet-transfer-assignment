package com.viditec.wallettransfer.api.response;

public record WalletResponse(
        String walletId,
        long balance
) {
}
