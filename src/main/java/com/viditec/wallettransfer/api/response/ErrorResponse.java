package com.viditec.wallettransfer.api.response;

public record ErrorResponse(
        String code,
        String message
) {
}
