package com.wallettransfer.web;

/** Error envelope: {@code {"error":{"code":"...","message":"..."}}}. */
public record ApiError(Detail error) {

  /** Machine readable {@code code} plus a human readable {@code message}. */
  public record Detail(String code, String message) {}

  static ApiError of(String code, String message) {
    return new ApiError(new Detail(code, message));
  }
}
