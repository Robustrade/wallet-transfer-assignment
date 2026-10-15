package com.wallettransfer.builder;

import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.dto.responsedto.ResponseDTO;

public final class ResponseBuilderFactory {

  private ResponseBuilderFactory() {}

  public static ResponseDTO getResponse(String message, String responseCode) {
    return getResponse(message, responseCode, null);
  }

  public static ResponseDTO getResponse(String message, String responseCode, Object data) {
    int code = parseResponseCode(responseCode);
    return ResponseDTO.builder()
        .status(isSuccessCode(code) ? ApplicationConstants.SUCCESS : ApplicationConstants.FAIL)
        .message(message)
        .responseCode(code)
        .data(data)
        .build();
  }

  private static int parseResponseCode(String responseCode) {
    try {
      return Integer.parseInt(responseCode);
    } catch (NumberFormatException ex) {
      throw new IllegalArgumentException("Invalid response code: " + responseCode, ex);
    }
  }

  private static boolean isSuccessCode(int code) {
    return code >= 20000 && code <= 100000;
  }
}
