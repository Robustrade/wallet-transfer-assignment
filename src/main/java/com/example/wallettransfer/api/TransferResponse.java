package com.example.wallettransfer.api;

import com.example.wallettransfer.application.TransferResult;
import com.example.wallettransfer.domain.TransferState;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.UUID;

@JsonInclude(JsonInclude.Include.NON_NULL)
public record TransferResponse(
        @JsonProperty("transferId") UUID transferId,
        @JsonProperty("state") TransferState state,
        @JsonProperty("fromWalletId") String fromWalletId,
        @JsonProperty("toWalletId") String toWalletId,
        @JsonProperty("amount") long amount,
        @JsonProperty("failureReason") String failureReason) {

    public static TransferResponse from(TransferResult result) {
        return new TransferResponse(
                result.transferId(),
                result.state(),
                result.fromWalletId(),
                result.toWalletId(),
                result.amount(),
                result.failureReason());
    }
}
