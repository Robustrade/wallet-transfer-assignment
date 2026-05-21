package com.kullu.wallet.dto.response;

import java.time.Instant;
import java.util.UUID;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.kullu.wallet.entity.TransferStatus;

import lombok.AllArgsConstructor;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class TransferResponse {

    private UUID transferId;
    private TransferStatus status;
    private String fromWalletId;
    private String toWalletId;
    private long amount;
    private String failureReason;
    private Instant createdAt;

}
