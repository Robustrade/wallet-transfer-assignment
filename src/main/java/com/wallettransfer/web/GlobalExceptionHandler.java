package com.wallettransfer.web;

import com.wallettransfer.service.exception.IdempotencyConflictException;
import com.wallettransfer.service.exception.InvalidRequestException;
import com.wallettransfer.service.exception.NotFoundException;
import java.time.Instant;
import java.util.Map;
import org.springframework.http.HttpStatus;
import org.springframework.http.ProblemDetail;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

/**
 * Maps domain and validation exceptions to HTTP problem responses.
 */
@RestControllerAdvice
public class GlobalExceptionHandler {

    /**
     * @param ex wallet or transfer not found
     * @return 404 problem detail
     */
    @ExceptionHandler(NotFoundException.class)
    public ProblemDetail handleNotFound(NotFoundException ex) {
        return problem(HttpStatus.NOT_FOUND, ex.getMessage());
    }

    /**
     * @param ex invalid business input (e.g. same wallet, non-positive amount)
     * @return 400 problem detail
     */
    @ExceptionHandler(InvalidRequestException.class)
    public ProblemDetail handleInvalidRequest(InvalidRequestException ex) {
        return problem(HttpStatus.BAD_REQUEST, ex.getMessage());
    }

    /**
     * @param ex idempotency key reused with a different payload
     * @return 409 problem detail
     */
    @ExceptionHandler(IdempotencyConflictException.class)
    public ProblemDetail handleIdempotencyConflict(IdempotencyConflictException ex) {
        return problem(HttpStatus.CONFLICT, ex.getMessage());
    }

    /**
     * @param ex Bean Validation failures on request DTOs
     * @return 400 with first field error message
     */
    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ProblemDetail handleValidation(MethodArgumentNotValidException ex) {
        String message = ex.getBindingResult().getFieldErrors().stream()
                .findFirst()
                .map(err -> err.getField() + ": " + err.getDefaultMessage())
                .orElse("validation failed");
        return problem(HttpStatus.BAD_REQUEST, message);
    }

    /**
     * Builds a RFC 7807-style problem response with timestamp.
     */
    private ProblemDetail problem(HttpStatus status, String detail) {
        ProblemDetail problem = ProblemDetail.forStatusAndDetail(status, detail);
        problem.setProperty("timestamp", Instant.now().toString());
        problem.setProperty("errors", Map.of());
        return problem;
    }
}
