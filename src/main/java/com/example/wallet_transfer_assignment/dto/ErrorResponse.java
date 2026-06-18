package com.example.wallet_transfer_assignment.dto;

import java.time.LocalDateTime;

public record ErrorResponse(

        String errorCode,

        String message,

        LocalDateTime timestamp

) {
}