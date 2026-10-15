package com.wallettransfer.configuration;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.service.loggerservice.LogService;
import com.wallettransfer.utility.LogSanitizer;
import jakarta.servlet.http.HttpServletRequest;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class GlobalRestExceptionHandler {

  private final String className = this.getClass().getSimpleName();
  private final LogService logService;
  private final HttpServletRequest request;

  public GlobalRestExceptionHandler(LogService logService, HttpServletRequest request) {
    this.logService = logService;
    this.request = request;
  }

  @ExceptionHandler(MethodArgumentNotValidException.class)
  public ResponseDTO handleValidationErrors(MethodArgumentNotValidException ex) {
    String message =
        ex.getBindingResult().getFieldErrors().stream()
            .map(error -> error.getField() + ": " + error.getDefaultMessage())
            .findFirst()
            .orElse(ApplicationConstants.MANDATORY_PARAMETER_VALIDATION_FAILURE);
    return ResponseBuilderFactory.getResponse(message, ResponseConstant.INVALID_REQUEST);
  }

  @ExceptionHandler(HttpMessageNotReadableException.class)
  public ResponseDTO handleHttpMessageNotReadable(HttpMessageNotReadableException ex) {
    logService.printErrorLogs(request, className, ex);
    return ResponseBuilderFactory.getResponse(
        ApplicationConstants.FAIL, ResponseConstant.FAILURE_CODE);
  }

  @ExceptionHandler(ResponseException.class)
  public ResponseDTO handleResponseException(ResponseException ex) {
    logService.printResponseErrorLogs(request, className, LogSanitizer.sanitize(ex.getMessage()));
    return ResponseBuilderFactory.getResponse(ex.getMessage(), ex.getResponseCode(), ex.getData());
  }

  @ExceptionHandler(Exception.class)
  public ResponseDTO handleUnexpectedException(Exception ex) {
    logService.printErrorLogs(request, className, ex);
    return ResponseBuilderFactory.getResponse(
        ApplicationConstants.FAIL, ResponseConstant.FAILURE_CODE);
  }
}
