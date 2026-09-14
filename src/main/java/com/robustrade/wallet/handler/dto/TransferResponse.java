package com.robustrade.wallet.handler.dto;

import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.domain.TransferState;
import java.math.BigDecimal;
import java.util.UUID;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class TransferResponse {

    private UUID transferId;
    private String idempotencyKey;
    private String fromWalletId;
    private String toWalletId;
    private BigDecimal amount;
    private TransferState state;
    private String failureReason;

    public static TransferResponse from(Transfer transfer) {
        return TransferResponse.builder()
                .transferId(transfer.getId())
                .idempotencyKey(transfer.getIdempotencyKey())
                .fromWalletId(transfer.getFromWallet())
                .toWalletId(transfer.getToWallet())
                .amount(transfer.getAmount())
                .state(transfer.getState())
                .failureReason(
                        transfer.getState() == TransferState.FAILED ? transfer.getFailureReason() : null
                )
                .build();
    }
}
