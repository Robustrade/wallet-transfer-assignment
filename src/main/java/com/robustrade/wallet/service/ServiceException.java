package com.robustrade.wallet.service;

import lombok.Getter;

@Getter
public class ServiceException extends RuntimeException {

    private final String errorCode;
    private final int httpStatus;

    public ServiceException(String errorCode, String message, int httpStatus) {
        super(message);
        this.errorCode = errorCode;
        this.httpStatus = httpStatus;
    }
}
