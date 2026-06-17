package com.siddharth.wallet.dto;

import java.time.Instant;

public record ErrorResponse(
        String message,
        Instant timestamp
) {}