package com.example.wallettransfer.api;

import com.fasterxml.jackson.annotation.JsonProperty;

public record WalletResponse(
        @JsonProperty("id") String id,
        @JsonProperty("balance") long balance) {
}
