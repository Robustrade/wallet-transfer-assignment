package com.wallettransfer.exception;

import java.io.Serializable;
import lombok.Getter;

@Getter
public class ResponseException extends RuntimeException {

  private static final long serialVersionUID = 1L;
  private final String responseCode;
  private final Serializable data;

  public ResponseException(String message, String responseCode) {
    this(message, responseCode, null);
  }

  public ResponseException(String message, String responseCode, Object data) {
    super(message);
    this.responseCode = responseCode;
    this.data = data instanceof Serializable serializableData ? serializableData : null;
  }
}
