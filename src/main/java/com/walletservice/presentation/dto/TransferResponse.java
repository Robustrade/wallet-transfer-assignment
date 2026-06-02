package com.walletservice.presentation.dto;

import com.walletservice.domain.entity.Transfer.TransferStatus;
import lombok.*;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Response body for transfer creation.
 */
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class TransferResponse {
    private String id;
    private String fromWalletId;
    private String toWalletId;
    private Long amount;
    private TransferStatus status;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}

