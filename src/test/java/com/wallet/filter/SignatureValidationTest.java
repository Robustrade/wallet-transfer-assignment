package com.wallet.filter;

import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.mock.web.MockHttpServletRequest;
import org.springframework.mock.web.MockHttpServletResponse;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.HexFormat;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.*;

/**
 * Unit tests for {@link SignatureFilter}: HMAC validation, timestamp TTL and
 * clock-skew enforcement, and missing-header behavior. Idempotency-Key shape
 * validation lives in {@link IdempotencyHeaderFilter} and is exercised by its
 * own test class.
 */
class SignatureValidationTest {

    private static final String SECRET = "my-super-secret-key-that-is-at-least-256-bits-long-for-hmac-sha256";
    /** Matches the production default (security.signature.ttl-minutes). */
    private static final long TTL_MINUTES = 5;

    private ObjectMapper objectMapper;
    private SignatureFilter filter;
    private FilterChain filterChain;

    @BeforeEach
    void setUp() {
        objectMapper = new ObjectMapper();
        filter = new SignatureFilter(objectMapper, true, SECRET, TTL_MINUTES);
        filterChain = mock(FilterChain.class);
    }

    @Test
    @DisplayName("Should pass when request has valid signature and timestamp")
    void shouldPassWithValidSignatureAndTimestamp() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        String clientId = "test-client";
        String idempotencyKey = "test-key-123";
        String timestamp = String.valueOf(Instant.now().toEpochMilli());
        String body = "{\"fromWalletId\":\"wallet_1\",\"toWalletId\":\"wallet_2\",\"amount\":100}";

        request.addHeader("X-Client-Id", clientId);
        request.addHeader("Idempotency-Key", idempotencyKey);
        request.addHeader("X-Timestamp", timestamp);
        request.setContent(body.getBytes(StandardCharsets.UTF_8));

        String expectedSignature = computeSignature("POST", "/transfers", clientId, idempotencyKey, timestamp, body);
        request.addHeader("X-Signature", expectedSignature);

        // Wrap to mirror production filter chain: IdempotencyHeaderFilter runs
        // before SignatureFilter and supplies the cached-body wrapper.
        CachedBodyHttpServletRequest wrapped = new CachedBodyHttpServletRequest(request);

        filter.doFilter(wrapped, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    @Test
    @DisplayName("Should fail when X-Client-Id header is missing")
    void shouldFailWhenClientIdIsMissing() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", String.valueOf(Instant.now().toEpochMilli()));

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("X-Client-Id header is required");
    }

    @Test
    @DisplayName("Should fail when X-Timestamp header is missing")
    void shouldFailWhenTimestampIsMissing() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("X-Timestamp header is required");
    }

    @Test
    @DisplayName("Should fail when X-Timestamp is malformed")
    void shouldFailWhenTimestampIsMalformed() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", "not-a-number");

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("X-Timestamp header must be a valid epoch millisecond timestamp");
    }

    @Test
    @DisplayName("Should fail when X-Timestamp is older than the configured TTL")
    void shouldFailWhenTimestampIsExpired() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        // One minute past the 5-minute window — comfortably expired regardless
        // of any small clock drift between Instant.now() calls.
        long expiredTime = Instant.now().minusSeconds((TTL_MINUTES + 1) * 60).toEpochMilli();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", String.valueOf(expiredTime));

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.UNAUTHORIZED.value());
        assertThat(response.getContentAsString()).contains("X-Timestamp is expired");
    }

    @Test
    @DisplayName("Should fail when X-Timestamp is in the future beyond clock skew allowance")
    void shouldFailWhenTimestampIsInTheFuture() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        // 10 minutes in the future
        long futureTime = Instant.now().plusSeconds(10 * 60).toEpochMilli();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", String.valueOf(futureTime));

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.UNAUTHORIZED.value());
        assertThat(response.getContentAsString()).contains("X-Timestamp is in the future");
    }

    @Test
    @DisplayName("Should fail when X-Signature header is missing")
    void shouldFailWhenSignatureIsMissing() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", String.valueOf(Instant.now().toEpochMilli()));
        request.setContent("{}".getBytes(StandardCharsets.UTF_8));

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("X-Signature header is required");
    }

    @Test
    @DisplayName("Should fail when X-Signature is invalid")
    void shouldFailWhenSignatureIsInvalid() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        request.addHeader("X-Client-Id", "test-client");
        request.addHeader("Idempotency-Key", "test-key");
        request.addHeader("X-Timestamp", String.valueOf(Instant.now().toEpochMilli()));
        request.setContent("{}".getBytes(StandardCharsets.UTF_8));
        request.addHeader("X-Signature", "wrong-signature-value");

        // Wrap to mirror production filter chain: IdempotencyHeaderFilter runs
        // before SignatureFilter and supplies the cached-body wrapper.
        CachedBodyHttpServletRequest wrapped = new CachedBodyHttpServletRequest(request);

        filter.doFilter(wrapped, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.UNAUTHORIZED.value());
        assertThat(response.getContentAsString()).contains("Invalid X-Signature");
    }

    @Test
    @DisplayName("Should bypass validation for non-mutating or non-idempotent endpoints")
    void shouldBypassValidationForNonIdempotentEndpoints() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("GET", "/wallets/wallet_1/balance");
        MockHttpServletResponse response = new MockHttpServletResponse();

        filter.doFilter(request, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    @Test
    @DisplayName("Should bypass validation when signing is disabled")
    void shouldBypassValidationWhenSigningDisabled() throws ServletException, IOException {
        SignatureFilter disabledFilter = new SignatureFilter(objectMapper, false, SECRET, TTL_MINUTES);

        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();

        // No HMAC headers at all — disabled filter should still pass through.
        disabledFilter.doFilter(request, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    private String computeSignature(String method, String path, String clientId, String key, String timestamp, String body) {
        try {
            String bodyHash = sha256Hex(body);
            SecretKeySpec secretKeySpec = new SecretKeySpec(SECRET.getBytes(StandardCharsets.UTF_8), "HmacSHA256");
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(secretKeySpec);
            String data = method + ":" + path + ":" + clientId + ":" + key + ":" + timestamp + ":" + bodyHash;
            byte[] hmacBytes = mac.doFinal(data.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hmacBytes);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    private static String sha256Hex(String input) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(input.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hash);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }
}
