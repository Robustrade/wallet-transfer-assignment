package com.kullu.wallet.dto.response;

import com.kullu.wallet.entity.IdempotencyEntity;

public record IdempotencyResponse(
    String requestHash,
    Short responseStatus,
    String responseBody
) {

    public IdempotencyResponse {
        if (requestHash == null || requestHash.length() != 64) {
            throw new IllegalArgumentException("requestHash must be 64 chars");
        }
    }

    public IdempotencyResponse(final IdempotencyEntity idempotencyEntity) {
        this(
                idempotencyEntity.getRequestHash(),
                idempotencyEntity.getResponseStatus(),
                idempotencyEntity.getResponseBody()
        );
    }

    public boolean hasResponse() {
        return responseStatus != null && responseBody != null;
    }
}
