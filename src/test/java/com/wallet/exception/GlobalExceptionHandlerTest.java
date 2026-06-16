package com.wallet.exception;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.dao.CannotAcquireLockException;
import org.springframework.dao.DataAccessResourceFailureException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import java.sql.SQLException;
import java.util.Map;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Unit tests for the SQLState-driven status-code mapping in
 * {@link GlobalExceptionHandler}. The integration suite (notably
 * {@code TransactionTimeoutIntegrationTest} and
 * {@code LockTimeoutIntegrationTest}) covers the end-to-end path with a real
 * Postgres; this class locks down the in-process logic so a regression on
 * either branch is caught without spinning up Testcontainers.
 *
 * <p>Two paths exercised here:
 * <ul>
 *   <li><b>Typed handler</b> — Spring's PostgreSQL vendor codes translate
 *       SQLState {@code 55P03} ({@code lock_not_available}) directly to
 *       {@link CannotAcquireLockException}. This is the primary route on the
 *       normal Spring path; if {@link GlobalExceptionHandler#handleCannotAcquireLock}
 *       is removed or returns the wrong status, {@code lock_timeout} starts
 *       surfacing as 500.</li>
 *   <li><b>SQLState walk (defense-in-depth)</b> — {@code 55P03} arriving
 *       wrapped as {@link DataAccessResourceFailureException} (e.g. via a
 *       custom {@code SQLExceptionTranslator} or future Spring routing
 *       change) is still mapped to 504 by
 *       {@code handleResourceFailure}'s SQLState walk.</li>
 * </ul>
 *
 * <p>Negative tests guard against over-matching: a non-timeout SQLState
 * (e.g. {@code 23505} {@code unique_violation}) wrapped in
 * {@code DataAccessResourceFailureException} must still surface as 500, not 504.
 */
class GlobalExceptionHandlerTest {

    private final GlobalExceptionHandler handler = new GlobalExceptionHandler();

    @Test
    @DisplayName("CannotAcquireLockException (lock_timeout / 55P03 → typed) returns 504")
    void cannotAcquireLockReturns504() {
        SQLException sqlEx = new SQLException("ERROR: canceling statement due to lock timeout", "55P03");
        CannotAcquireLockException ex = new CannotAcquireLockException("lock_timeout fired", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleCannotAcquireLock(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().get("statusCode")).isEqualTo(HttpStatus.GATEWAY_TIMEOUT.value());
        assertThat(response.getBody().get("message").toString()).contains("timed out");
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with SQLState 55P03 returns 504 (defense-in-depth)")
    void resourceFailureWith55P03Returns504() {
        // A non-standard wrapping path where 55P03 reaches handleResourceFailure
        // instead of the typed CannotAcquireLockException handler. The SQLState
        // walk in handleResourceFailure is the safety net.
        SQLException sqlEx = new SQLException("ERROR: canceling statement due to lock timeout", "55P03");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("wrapped 55P03", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode())
                .as("55P03 must map to 504 even on the defense-in-depth (SQLState walk) path")
                .isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with SQLState 57014 (statement_timeout) returns 504")
    void resourceFailureWith57014Returns504() {
        SQLException sqlEx = new SQLException("ERROR: canceling statement due to statement timeout", "57014");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("wrapped 57014", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with SQLState 57P01 (admin_shutdown) returns 504")
    void resourceFailureWith57P01Returns504() {
        SQLException sqlEx = new SQLException("ERROR: terminating connection due to administrator command", "57P01");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("admin shutdown", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with SQLState 08006 (connection_failure) returns 504")
    void resourceFailureWith08006Returns504() {
        SQLException sqlEx = new SQLException("connection failure", "08006");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("connection failure", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with non-timeout SQLState (23505) returns 500")
    void resourceFailureWithUnrelatedStateReturns500() {
        // 23505 = unique_violation. Should NOT be lumped into the timeout
        // arm. Guards against future "let's just return 504 for everything"
        // refactors that would mask real bugs.
        SQLException sqlEx = new SQLException("duplicate key", "23505");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("dup key wrapped", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode())
                .as("non-timeout SQLState must surface as 500, not 504 — over-matching the timeout arm masks real failures")
                .isEqualTo(HttpStatus.INTERNAL_SERVER_ERROR);
    }

    @Test
    @DisplayName("DataAccessResourceFailureException with class-55 non-timeout state (55006) returns 500")
    void resourceFailureWith55006Returns500() {
        // 55006 = object_in_use, also class 55 but NOT a timeout. Asserts the
        // explicit "55P03".equals() match instead of startsWith("55") so we
        // don't over-match other class-55 states.
        SQLException sqlEx = new SQLException("object in use", "55006");
        DataAccessResourceFailureException ex =
                new DataAccessResourceFailureException("object in use", sqlEx);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(ex);

        assertThat(response.getStatusCode())
                .as("class-55 states other than 55P03 must NOT match the timeout arm")
                .isEqualTo(HttpStatus.INTERNAL_SERVER_ERROR);
    }

    @Test
    @DisplayName("Cause-chain walk: nested SQLException with 55P03 still resolves to 504")
    void resourceFailureWithNestedCauseChainReturns504() {
        // Real Spring exception translation often wraps the SQLException
        // several levels deep. The SQLState walk must follow getCause() to
        // find it.
        SQLException leaf = new SQLException("lock not available", "55P03");
        RuntimeException intermediate = new RuntimeException("wrapping layer", leaf);
        DataAccessResourceFailureException outer =
                new DataAccessResourceFailureException("outer", intermediate);

        ResponseEntity<Map<String, Object>> response = handler.handleResourceFailure(outer);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.GATEWAY_TIMEOUT);
    }
}
