package com.robustrade.wallet_transfer_service.dto;

import com.robustrade.wallet_transfer_service.enums.TransferStatus;
import lombok.Builder;
import lombok.Data;

@Data
@Builder
public class TransferResponse {

    private Long transferId;
    private TransferStatus status;
    private Long amount;
}