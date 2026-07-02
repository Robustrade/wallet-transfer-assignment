package com.wallettransfer.constants;

public final class ResponseConstant {

  private ResponseConstant() {}

  public static final String FAILURE_CODE = "5000";
  public static final String SUCCESS_CODE = "20000";

  public static final String TRANSFER_SUCCESS = "20001";
  public static final String BALANCE_FETCH_SUCCESS = "20002";
  public static final String HISTORY_FETCH_SUCCESS = "20003";

  public static final String INVALID_REQUEST = "6017";
  public static final String TRANSFER_FAILED = "5001";
  public static final String INSUFFICIENT_FUNDS = "5002";
  public static final String WALLET_NOT_FOUND = "5113";
  public static final String IDEMPOTENCY_CONFLICT = "5003";
}
