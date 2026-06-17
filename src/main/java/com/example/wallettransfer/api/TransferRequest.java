package com.example.wallettransfer.api;

import com.fasterxml.jackson.annotation.JsonProperty;

public record TransferRequest(
        @JsonProperty("idempotencyKey") String idempotencyKey,
        @JsonProperty("fromWalletId") String fromWalletId,
        @JsonProperty("toWalletId") String toWalletId,
        @JsonProperty("amount") long amount) {
}
