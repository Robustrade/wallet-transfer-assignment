package com.wallet.filter;

import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

/**
 * Rejects absent, blank, or oversized {@code Idempotency-Key} headers on
 * state-mutating endpoints. Runs before {@link SignatureFilter} so a missing
 * key fails fast even when signing is enabled.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 10)
public class IdempotencyHeaderFilter extends OncePerRequestFilter {

    static final String IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";
    static final int MAX_KEY_LENGTH = 256;

    /**
     * Mutating paths that require idempotency-key enforcement (and HMAC
     * validation downstream). Compared against a normalized request path —
     * see {@link #normalizePath(String)} — so trailing-slash, doubled-slash,
     * and case-only variants ({@code /transfers/}, {@code //transfers},
     * {@code /Transfers}) cannot silently bypass the gate. A bypass here
     * would let a controller-bound request reach
     * {@link com.wallet.controller.TransferController} without an
     * idempotency key or HMAC signature.
     *
     * <p><strong>Invariant:</strong> entries here MUST be lowercase.
     * {@link #normalizePath(String)} force-lowercases the request path before
     * the membership check, so an entry like {@code "/Transfers/Bulk"} would
     * silently never match and the route would bypass enforcement.
     */
    static final Set<String> IDEMPOTENT_ENDPOINTS = Set.of("/transfers");

    /**
     * Pre-compiled once and reused per request. {@code String.replaceAll(...)}
     * compiles its regex on every call — invisible at low traffic, but this
     * filter runs on every inbound request, so caching the {@link Pattern}
     * costs nothing and keeps regex compilation off the hot path.
     */
    private static final Pattern MULTI_SLASH = Pattern.compile("/+");

    private final ObjectMapper objectMapper;

    public IdempotencyHeaderFilter(ObjectMapper objectMapper) {
        this.objectMapper = objectMapper;
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request,
                                    HttpServletResponse response,
                                    FilterChain filterChain) throws ServletException, IOException {
        if (!requiresIdempotencyKey(request)) {
            filterChain.doFilter(request, response);
            return;
        }

        // Cache the body once so downstream filters/handlers can re-read it.
        CachedBodyHttpServletRequest cachedRequest = new CachedBodyHttpServletRequest(request);

        String key = cachedRequest.getHeader(IDEMPOTENCY_KEY_HEADER);
        if (key == null || key.trim().isEmpty()) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST, "Idempotency-Key header is required");
            return;
        }
        if (key.length() > MAX_KEY_LENGTH) {
            writeErrorResponse(response, HttpStatus.BAD_REQUEST,
                    "Idempotency-Key header exceeds maximum length of " + MAX_KEY_LENGTH + " characters");
            return;
        }

        filterChain.doFilter(cachedRequest, response);
    }

    static boolean requiresIdempotencyKey(HttpServletRequest request) {
        String method = request.getMethod();
        String path = normalizePath(request.getRequestURI());

        boolean isMutatingMethod = "POST".equalsIgnoreCase(method)
                || "PUT".equalsIgnoreCase(method)
                || "PATCH".equalsIgnoreCase(method);

        return isMutatingMethod && IDEMPOTENT_ENDPOINTS.contains(path);
    }

    /**
     * Lower-cases, collapses any run of {@code /} characters to a single
     * {@code /}, and strips a trailing slash (preserving the bare {@code /}
     * root). Defensive against three variants that an unnormalized exact-match
     * predicate would silently let through:
     * <ul>
     *   <li>{@code /transfers/} — trailing slash</li>
     *   <li>{@code //transfers} — collapsed double slash</li>
     *   <li>{@code /Transfers} — case-only difference</li>
     * </ul>
     *
     * <p>The fix errs on the side of enforcement: if the normalized path
     * resolves to a mutating endpoint, the filter requires the idempotency
     * key. If Spring's routing then declines the request (default trailing-
     * slash and case-sensitivity rules), the response is a 404 — the filter
     * has done its job and there is no bypass to worry about.
     *
     * <p>Returns {@code null} or empty input unchanged so the caller's
     * {@link Set#contains(Object)} check fails closed (i.e. treats it as a
     * non-mutating endpoint, which lets unrelated paths flow through).
     */
    private static String normalizePath(String path) {
        if (path == null || path.isEmpty()) {
            return path;
        }
        String normalized = path.toLowerCase(Locale.ROOT);
        // Collapse any "//+" run to a single slash via the pre-compiled pattern.
        normalized = MULTI_SLASH.matcher(normalized).replaceAll("/");
        // Strip a single trailing slash, but keep "/" itself.
        if (normalized.length() > 1 && normalized.endsWith("/")) {
            normalized = normalized.substring(0, normalized.length() - 1);
        }
        return normalized;
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
