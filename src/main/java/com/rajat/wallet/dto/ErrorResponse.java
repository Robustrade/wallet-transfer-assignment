package com.rajat.wallet.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import java.time.Instant;
import java.util.Map;

/** Uniform error body for all non-2xx responses. {@code fieldErrors} is present only for 400s. */
@JsonInclude(JsonInclude.Include.NON_NULL)
public record ErrorResponse(
    Instant timestamp, int status, String error, String message, Map<String, String> fieldErrors) {

  public static ErrorResponse of(int status, String error, String message) {
    return new ErrorResponse(Instant.now(), status, error, message, null);
  }

  public static ErrorResponse of(
      int status, String error, String message, Map<String, String> fieldErrors) {
    return new ErrorResponse(Instant.now(), status, error, message, fieldErrors);
  }
}
