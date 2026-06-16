package com.wallet.filter;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.slf4j.MDC;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.util.UUID;

/**
 * Populates MDC with a fresh requestId, a correlationId (taken from the
 * inbound header or generated), and the {@code X-Client-Id} header value
 * when present. Runs at HIGHEST_PRECEDENCE so every downstream filter,
 * controller, and framework log line carries the same tracing context.
 *
 * <p>The clientId enrichment is opportunistic: a missing/blank header
 * leaves the MDC slot unset, which renders as an empty value (or the
 * configured default — see the {@code logging.pattern.console} entry
 * in {@code application.yml}) rather than {@code null}. This keeps
 * health checks and other unauthenticated paths quiet in the logs
 * while every authenticated request gets {@code clientId} on every
 * line for free, including framework logs (Hikari, jOOQ, Spring) that
 * never had access to the controller-level argument.
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE)
public class TracingFilter extends OncePerRequestFilter {

    private static final String CORRELATION_ID_HEADER = "X-Correlation-Id";
    private static final String REQUEST_ID_HEADER = "X-Request-Id";
    private static final String CLIENT_ID_HEADER = "X-Client-Id";

    private static final String MDC_CORRELATION_ID = "correlationId";
    private static final String MDC_REQUEST_ID = "requestId";
    private static final String MDC_CLIENT_ID = "clientId";

    @Override
    protected void doFilterInternal(HttpServletRequest request,
                                    HttpServletResponse response,
                                    FilterChain filterChain) throws ServletException, IOException {

        String correlationId = request.getHeader(CORRELATION_ID_HEADER);
        if (correlationId == null || correlationId.trim().isEmpty()) {
            correlationId = UUID.randomUUID().toString();
        }

        String requestId = UUID.randomUUID().toString();

        MDC.put(MDC_CORRELATION_ID, correlationId);
        MDC.put(MDC_REQUEST_ID, requestId);

        // Pulled at the tracing layer rather than after authentication so
        // even rejected requests (e.g. unknown client → 403) carry the
        // claimed clientId in their log lines, which is exactly the value
        // an oncall debugging "why is client X getting 403s" needs.
        String clientId = request.getHeader(CLIENT_ID_HEADER);
        if (clientId != null && !clientId.isBlank()) {
            MDC.put(MDC_CLIENT_ID, clientId);
        }

        // Echo on the response so clients can correlate.
        response.setHeader(CORRELATION_ID_HEADER, correlationId);
        response.setHeader(REQUEST_ID_HEADER, requestId);

        try {
            filterChain.doFilter(request, response);
        } finally {
            // Clears all three slots — including clientId — so a pooled
            // request thread cannot leak context into the next request.
            MDC.clear();
        }
    }
}
