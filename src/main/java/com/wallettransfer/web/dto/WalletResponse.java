package com.wallettransfer.web.dto;

import com.wallettransfer.domain.Wallet;
import java.time.Instant;

/**
 * API response for wallet balance and metadata.
 */
public record WalletResponse(String id, long balance, Instant createdAt, Instant updatedAt) {

    /**
     * @param wallet persisted wallet entity
     * @return API response DTO
     */
    public static WalletResponse from(Wallet wallet) {
        return new WalletResponse(
                wallet.getId(), wallet.getBalance(), wallet.getCreatedAt(), wallet.getUpdatedAt());
    }
}
