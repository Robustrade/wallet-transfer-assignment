package com.kullu.wallet.dto.request;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import lombok.Getter;

@Getter
public class CreateTransferRequest {

    @NotBlank
    @Size(max = 64)
    private final String idempotencyKey;

    @NotBlank
    @Size(max = 64)
    private final String fromWalletId;

    @NotBlank
    @Size(max = 64)
    private final String toWalletId;

    @NotNull
    @Positive
    private final Long amount;

    @JsonCreator
    public CreateTransferRequest(
        @JsonProperty("idempotencyKey") final String idempotencyKey,
        @JsonProperty("fromWalletId") final String fromWalletId,
        @JsonProperty("toWalletId") final String toWalletId,
        @JsonProperty("amount") final Long amount) {
        this.idempotencyKey = idempotencyKey;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
    }
}
