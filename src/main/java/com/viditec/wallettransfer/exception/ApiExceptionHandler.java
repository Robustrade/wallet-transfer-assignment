package com.viditec.wallettransfer.exception;

import com.viditec.wallettransfer.api.response.ErrorResponse;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class ApiExceptionHandler {

    @ExceptionHandler(DuplicateIdempotencyKeyException.class)
    public ResponseEntity<ErrorResponse> handleDuplicateIdempotency(DuplicateIdempotencyKeyException exception) {
        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
    }

    @ExceptionHandler({InvalidTransferRequestException.class, MethodArgumentNotValidException.class})
    public ResponseEntity<ErrorResponse> handleBadRequest(Exception exception) {
        String message = exception instanceof TransferException transferException
                ? transferException.getMessage()
                : "Request validation failed.";
        String code = exception instanceof TransferException transferException
                ? transferException.getCode()
                : "VALIDATION_ERROR";
        return ResponseEntity.badRequest().body(new ErrorResponse(code, message));
    }

    @ExceptionHandler(ResourceNotFoundException.class)
    public ResponseEntity<ErrorResponse> handleNotFound(ResourceNotFoundException exception) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
    }
}
