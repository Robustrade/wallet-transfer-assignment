package com.wallet.exception;

import com.wallet.util.LogSafe;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.sql.SQLException;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.stream.Collectors;

/**
 * Global exception handler mapping domain exceptions to appropriate HTTP responses.
 */
@RestControllerAdvice
public class GlobalExceptionHandler {

    private static final Logger log = LoggerFactory.getLogger(GlobalExceptionHandler.class);

    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<Map<String, Object>> handleWalletNotFound(WalletNotFoundException ex) {
        log.warn("Wallet not found: {}", ex.getWalletId());
        return buildResponse(HttpStatus.NOT_FOUND, ex.getMessage());
    }

    @ExceptionHandler(InsufficientBalanceException.class)
    public ResponseEntity<Map<String, Object>> handleInsufficientBalance(InsufficientBalanceException ex) {
        log.warn("Insufficient balance: wallet={}, available={}, requested={}",
                ex.getWalletId(), ex.getCurrentBalance(), ex.getRequestedAmount());
        return buildResponse(HttpStatus.UNPROCESSABLE_ENTITY, ex.getMessage());
    }

    @ExceptionHandler(IdempotencyConflictException.class)
    public ResponseEntity<Map<String, Object>> handleIdempotencyConflict(IdempotencyConflictException ex) {
        log.warn("Idempotency conflict: key={}", LogSafe.mask(ex.getIdempotencyKey()));
        return buildResponse(HttpStatus.CONFLICT, ex.getMessage());
    }

    @ExceptionHandler(IdempotencyKeyInProgressException.class)
    public ResponseEntity<Map<String, Object>> handleIdempotencyKeyInProgress(IdempotencyKeyInProgressException ex) {
        log.warn("Idempotency key in progress: key={}", LogSafe.mask(ex.getIdempotencyKey()));
        return buildResponse(HttpStatus.CONFLICT, ex.getMessage());
    }

    /**
     * Transient retry-loop exhaustion is server-side contention, not a caller
     * mistake — surface as 503 with Retry-After so dashboards and clients see
     * "back off, system is hot" instead of the 409 body-mismatch story they'd
     * otherwise misread.
     */
    @ExceptionHandler(TransientIdempotencyConflictException.class)
    public ResponseEntity<Map<String, Object>> handleTransientIdempotencyConflict(TransientIdempotencyConflictException ex) {
        log.warn("Transient idempotency conflict (retry budget exhausted): key={}",
                LogSafe.mask(ex.getIdempotencyKey()));
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("timestamp", Instant.now().toString());
        body.put("statusCode", HttpStatus.SERVICE_UNAVAILABLE.value());
        body.put("error", HttpStatus.SERVICE_UNAVAILABLE.getReasonPhrase());
        body.put("message", ex.getMessage());
        return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
                .header(HttpHeaders.RETRY_AFTER, Integer.toString(ex.getRetryAfterSeconds()))
                .body(body);
    }

    @ExceptionHandler(UnknownClientException.class)
    public ResponseEntity<Map<String, Object>> handleUnknownClient(UnknownClientException ex) {
        log.warn("Rejected request from unknown client: {}", ex.getClientId());
        return buildResponse(HttpStatus.FORBIDDEN, ex.getMessage());
    }

    /**
     * Service-layer parameter rejections (e.g. out-of-range pagination limits)
     * surface as 400 — the request was syntactically parseable but semantically
     * out of contract. The transfer-domain cross-field rule (same source and
     * destination wallet) is intentionally not raised through this handler:
     * it lives on {@link com.wallet.controller.dto.CreateTransferRequest} as
     * an {@code @AssertTrue} method and rides {@link #handleValidation} below.
     * A dedicated {@code InvalidTransferException} thrown from a separate
     * validator was considered but rejected — it would split the request-
     * validation surface across two paths (custom exception + Bean Validation)
     * and require ordering guarantees the framework doesn't give for free.
     * Routing every validation through one path keeps the 400 contract uniform.
     */
    @ExceptionHandler(IllegalArgumentException.class)
    public ResponseEntity<Map<String, Object>> handleIllegalArgument(IllegalArgumentException ex) {
        log.warn("Illegal argument: {}", ex.getMessage());
        return buildResponse(HttpStatus.BAD_REQUEST, ex.getMessage());
    }

    @ExceptionHandler(org.springframework.web.method.annotation.MethodArgumentTypeMismatchException.class)
    public ResponseEntity<Map<String, Object>> handleTypeMismatch(
            org.springframework.web.method.annotation.MethodArgumentTypeMismatchException ex) {
        log.warn("Type mismatch on parameter '{}': {}", ex.getName(), ex.getMessage());
        return buildResponse(HttpStatus.BAD_REQUEST,
                "Parameter '" + ex.getName() + "' has invalid value");
    }

    /**
     * The configured {@code message} on each constraint is already
     * self-describing ({@code "fromWalletId is required"},
     * {@code "Source and destination wallet cannot be the same"}, etc.), so
     * we emit messages alone — prefixing with the field name produced
     * stuttering ({@code "fromWalletId: fromWalletId is required"}) and
     * leaked the synthetic {@code walletPairValid} property derived from the
     * {@code @AssertTrue} accessor on the cross-field rule. Multiple errors
     * are joined deterministically (sorted) so contract tests get a stable
     * 400 body even when the binding result iterates fields in an
     * implementation-defined order.
     */
    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<Map<String, Object>> handleValidation(MethodArgumentNotValidException ex) {
        String errors = ex.getBindingResult().getFieldErrors().stream()
                .map(fe -> fe.getDefaultMessage())
                .filter(msg -> msg != null && !msg.isBlank())
                .sorted()
                .collect(Collectors.joining(", "));
        log.warn("Validation failed: {}", errors);
        return buildResponse(HttpStatus.BAD_REQUEST, errors);
    }

    @ExceptionHandler(org.springframework.web.bind.MissingRequestHeaderException.class)
    public ResponseEntity<Map<String, Object>> handleMissingHeader(org.springframework.web.bind.MissingRequestHeaderException ex) {
        log.warn("Missing required header: {}", ex.getHeaderName());
        return buildResponse(HttpStatus.BAD_REQUEST, "Required header '" + ex.getHeaderName() + "' is missing");
    }

    @ExceptionHandler(org.springframework.transaction.TransactionTimedOutException.class)
    public ResponseEntity<Map<String, Object>> handleTransactionTimeout(org.springframework.transaction.TransactionTimedOutException ex) {
        log.warn("Transaction timed out: {}", ex.getMessage());
        return buildResponse(HttpStatus.GATEWAY_TIMEOUT, "The transfer transaction timed out. Please retry the request.");
    }

    @ExceptionHandler(org.springframework.dao.QueryTimeoutException.class)
    public ResponseEntity<Map<String, Object>> handleQueryTimeout(org.springframework.dao.QueryTimeoutException ex) {
        log.warn("Database query timed out: {}", ex.getMessage());
        return buildResponse(HttpStatus.GATEWAY_TIMEOUT, "The transfer transaction timed out. Please retry the request.");
    }

    /**
     * Postgres' {@code lock_timeout} fires SQLState {@code 55P03}
     * ({@code lock_not_available}). Spring's PostgreSQL vendor-code translator
     * (sql-error-codes.xml: {@code cannotAcquireLockCodes=55P03}) routes this
     * to {@link org.springframework.dao.CannotAcquireLockException} — NOT to
     * {@link org.springframework.dao.DataAccessResourceFailureException} —
     * so the SQLState walk in {@link #handleResourceFailure} would never see
     * it. From the client's perspective a lock-wait timeout is functionally a
     * timeout: the request transaction has rolled back, no committed state
     * was left behind, and a retry on the same idempotency key is safe. Map
     * to 504 so client retry libraries treat it as retryable in the same way
     * they treat {@code statement_timeout}; the symmetry between the two
     * timeout configurations should not leak through to a different status
     * code purely because Postgres reports them under different SQLState
     * classes.
     */
    @ExceptionHandler(org.springframework.dao.CannotAcquireLockException.class)
    public ResponseEntity<Map<String, Object>> handleCannotAcquireLock(org.springframework.dao.CannotAcquireLockException ex) {
        log.warn("Could not acquire row lock within lock_timeout: {}", ex.getMessage());
        return buildResponse(HttpStatus.GATEWAY_TIMEOUT, "The transfer transaction timed out. Please retry the request.");
    }

    /**
     * Discriminates 504 (timeout/cancellation) from 500 (other resource failures)
     * by inspecting the {@link SQLException#getSQLState() SQLState} on the cause
     * chain rather than string-matching the message — message text is
     * locale/driver-version-sensitive and would silently flip the response code
     * on a future driver upgrade. Codes recognised:
     * <ul>
     *   <li>{@code 57014} — query_canceled (statement_timeout fired)</li>
     *   <li>{@code 57P0x} — operator_intervention (admin_shutdown, crash_shutdown, etc.)</li>
     *   <li>{@code 08xxx} — connection_exception (any class-08 SQLState)</li>
     *   <li>{@code 55P03} — lock_not_available (lock_timeout fired) — defense-in-depth.
     *       On the normal Spring translation path, {@code 55P03} arrives as
     *       {@link org.springframework.dao.CannotAcquireLockException} and is
     *       handled by {@link #handleCannotAcquireLock} above, never reaching
     *       this method. Kept here so a non-standard wrapping path still maps
     *       to 504. Matched explicitly rather than via {@code startsWith("55")}
     *       because class 55 also contains non-timeout states
     *       ({@code 55006} {@code object_in_use}, etc.).</li>
     * </ul>
     */
    @ExceptionHandler(org.springframework.dao.DataAccessResourceFailureException.class)
    public ResponseEntity<Map<String, Object>> handleResourceFailure(org.springframework.dao.DataAccessResourceFailureException ex) {
        log.warn("Database resource failure: {}", ex.getMessage());
        if (isTimeoutOrConnectionFailure(ex)) {
            return buildResponse(HttpStatus.GATEWAY_TIMEOUT, "The transfer transaction timed out. Please retry the request.");
        }
        return buildResponse(HttpStatus.INTERNAL_SERVER_ERROR, "An unexpected database error occurred");
    }

    private static boolean isTimeoutOrConnectionFailure(Throwable ex) {
        for (Throwable t = ex; t != null; t = t.getCause()) {
            if (t instanceof SQLException sqlEx) {
                String state = sqlEx.getSQLState();
                if (state == null) {
                    continue;
                }
                if ("57014".equals(state)            // query_canceled (statement_timeout)
                        || "55P03".equals(state)     // lock_not_available (lock_timeout)
                        || state.startsWith("57P")   // operator_intervention class
                        || state.startsWith("08")) { // connection_exception class
                    return true;
                }
            }
            if (t.getCause() == t) {
                break; // self-referencing cause guard
            }
        }
        return false;
    }

    @ExceptionHandler(Exception.class)
    public ResponseEntity<Map<String, Object>> handleGeneral(Exception ex) {
        log.error("Unexpected error", ex);
        return buildResponse(HttpStatus.INTERNAL_SERVER_ERROR, "An unexpected error occurred");
    }

    private ResponseEntity<Map<String, Object>> buildResponse(HttpStatus status, String message) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("timestamp", Instant.now().toString());
        body.put("statusCode", status.value());
        body.put("error", status.getReasonPhrase());
        body.put("message", message);
        return ResponseEntity.status(status).body(body);
    }
}
