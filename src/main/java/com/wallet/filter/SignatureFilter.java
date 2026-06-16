package com.wallet.filter;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.util.Hashing;
import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Validates HMAC signatures on state-mutating endpoints when
 * {@code security.signature.enabled} is true. Checks X-Client-Id, X-Timestamp,
 * X-Signature; the idempotency key is already validated upstream by
 * {@link IdempotencyHeaderFilter}. No-op when signing is disabled.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 20)
public class SignatureFilter extends OncePerRequestFilter {

    private static final Logger log = LoggerFactory.getLogger(SignatureFilter.class);

    private static final String IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";
    private static final String CLIENT_ID_HEADER = "X-Client-Id";
    private static final String TIMESTAMP_HEADER = "X-Timestamp";
    private static final String SIGNATURE_HEADER = "X-Signature";

    /** Tolerance for future-dated client timestamps. */
    private static final long CLOCK_SKEW_TOLERANCE_MILLIS = 5 * 60 * 1000L;

    private final ObjectMapper objectMapper;
    private final boolean signatureEnabled;
    private final String secret;
    private final long ttlMillis;

    public SignatureFilter(ObjectMapper objectMapper,
                           @Value("${security.signature.enabled:false}") boolean signatureEnabled,
                           @Value("${security.signature.secret:}") String secret,
                           @Value("${security.signature.ttl-minutes:5}") long ttlMinutes) {
        // Fail fast: HMAC against an empty key is trivially forgeable.
        if (signatureEnabled && (secret == null || secret.isBlank())) {
            throw new IllegalStateException(
                    "security.signature.enabled=true requires security.signature.secret to be set");
        }
        this.objectMapper = objectMapper;
        this.signatureEnabled = signatureEnabled;
        this.secret = secret;
        this.ttlMillis = ttlMinutes * 60 * 1000L;

        // Loud, single startup line so the operational mode is impossible to
        // miss in CI logs, container init output, and dashboards. The disabled
        // path is the dangerous one (any caller can mutate balances without an
        // HMAC), so it logs at WARN with an explicit "do not run in production"
        // marker — grep-friendly for log alerting. The enabled path logs at
        // INFO so ops can still confirm the secure mode is active.
        if (signatureEnabled) {
            log.info("security.signature.enabled=true (HMAC required on state-mutating endpoints)");
        } else {
            log.warn("security.signature.enabled=false. DO NOT run this configuration in production.");
        }
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request,
                                    HttpServletResponse response,
                                    FilterChain filterChain) throws ServletException, IOException {
        if (!signatureEnabled || !IdempotencyHeaderFilter.requiresIdempotencyKey(request)) {
            filterChain.doFilter(request, response);
            return;
        }

        String clientId = request.getHeader(CLIENT_ID_HEADER);
        if (clientId == null || clientId.trim().isEmpty()) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST, "X-Client-Id header is required");
            return;
        }

        String timestampStr = request.getHeader(TIMESTAMP_HEADER);
        if (timestampStr == null || timestampStr.trim().isEmpty()) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST, "X-Timestamp header is required");
            return;
        }

        long timestamp;
        try {
            timestamp = Long.parseLong(timestampStr);
        } catch (NumberFormatException e) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST,
                    "X-Timestamp header must be a valid epoch millisecond timestamp");
            return;
        }

        long now = Instant.now().toEpochMilli();
        if (now - timestamp > ttlMillis) {
            writeErrorResponse(response, HttpStatus.UNAUTHORIZED,
                    "X-Timestamp is expired (older than " + (ttlMillis / 60_000L) + " minutes)");
            return;
        }
        if (timestamp - now > CLOCK_SKEW_TOLERANCE_MILLIS) {
            writeErrorResponse(response, HttpStatus.UNAUTHORIZED,
                    "X-Timestamp is in the future (exceeds clock skew allowance)");
            return;
        }

        String signature = request.getHeader(SIGNATURE_HEADER);
        if (signature == null || signature.trim().isEmpty()) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST, "X-Signature header is required");
            return;
        }

        // Body is cached upstream by IdempotencyHeaderFilter so we can read it
        // here (for HMAC) and again in the controller (for JSON binding). If
        // that upstream wrap is ever bypassed via filter-order config or
        // wiring change, we MUST fail loudly: a bare
        // request.getInputStream().readAllBytes() fallback was considered and
        // rejected — it would consume the body here and break downstream JSON
        // binding with a confusing "request body is missing" response, which
        // is strictly worse than failing fast at this assertion.
        if (!(request instanceof CachedBodyHttpServletRequest cbr)) {
            throw new IllegalStateException(
                    "SignatureFilter requires a CachedBodyHttpServletRequest "
                            + "(IdempotencyHeaderFilter must run earlier). "
                            + "Check filter @Order configuration.");
        }
        byte[] cachedBody = cbr.getCachedBody();
        String body = new String(cachedBody, StandardCharsets.UTF_8);
        String key = request.getHeader(IDEMPOTENCY_KEY_HEADER);
        String method = request.getMethod().toUpperCase();
        String path = request.getRequestURI();
        String expectedSignature = computeHmacSha256(method, path, clientId, key, timestampStr, body);

        if (!constantTimeEquals(signature, expectedSignature)) {
            writeErrorResponse(response, HttpStatus.UNAUTHORIZED, "Invalid X-Signature");
            return;
        }

        filterChain.doFilter(request, response);
    }

    private String computeHmacSha256(String method, String path, String clientId, String key,
                                     String timestamp, String body) {
        try {
            String bodyHash = Hashing.sha256Hex(body);
            SecretKeySpec secretKeySpec = new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256");
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(secretKeySpec);
            String data = method + ":" + path + ":" + clientId + ":" + key + ":" + timestamp + ":" + bodyHash;
            byte[] hmacBytes = mac.doFinal(data.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(hmacBytes);
        } catch (Exception e) {
            throw new RuntimeException("Failed to calculate HMAC-SHA256 signature", e);
        }
    }

    private boolean constantTimeEquals(String a, String b) {
        if (a == null || b == null) {
            return false;
        }
        return MessageDigest.isEqual(a.getBytes(StandardCharsets.UTF_8), b.getBytes(StandardCharsets.UTF_8));
    }

    private void writeErrorResponse(HttpServletResponse response, HttpStatus status, String message) throws IOException {
        response.setStatus(status.value());
        response.setContentType(MediaType.APPLICATION_JSON_VALUE);

        Map<String, Object> body = new LinkedHashMap<>();
        body.put("timestamp", Instant.now().toString());
        body.put("statusCode", status.value());
        body.put("error", status.getReasonPhrase());
        body.put("message", message);

        objectMapper.writeValue(response.getOutputStream(), body);
    }
}
