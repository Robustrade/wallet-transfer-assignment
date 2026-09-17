package com.robustrade.wallet.dto;

import java.util.Map;

public record ErrorResponse(String error, String message, Map<String, String> details) {

  public static ErrorResponse of(String error, String message) {
    return new ErrorResponse(error, message, null);
  }
}
