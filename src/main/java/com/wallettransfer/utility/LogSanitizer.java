package com.wallettransfer.utility;

public final class LogSanitizer {

  private LogSanitizer() {}

  public static String sanitize(String value) {
    if (value == null) {
      return "";
    }
    return value.replaceAll("[\r\n\t]", "_");
  }
}
