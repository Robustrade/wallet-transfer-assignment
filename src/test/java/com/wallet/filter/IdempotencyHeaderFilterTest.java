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

import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.*;

/**
 * Unit tests for {@link IdempotencyHeaderFilter}: presence/length validation
 * of the {@code Idempotency-Key} header on state-mutating endpoints.
 */
class IdempotencyHeaderFilterTest {

    private IdempotencyHeaderFilter filter;
    private FilterChain filterChain;

    @BeforeEach
    void setUp() {
        filter = new IdempotencyHeaderFilter(new ObjectMapper());
        filterChain = mock(FilterChain.class);
    }

    @Test
    @DisplayName("Should pass when Idempotency-Key header is present")
    void shouldPassWhenKeyIsPresent() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.addHeader("Idempotency-Key", "abcdef-12345");
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    @Test
    @DisplayName("Should fail when Idempotency-Key header is missing")
    void shouldFailWhenKeyIsMissing() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("Idempotency-Key header is required");
    }

    @Test
    @DisplayName("Should fail when Idempotency-Key header is blank/whitespace-only")
    void shouldFailWhenKeyIsBlank() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.addHeader("Idempotency-Key", "   ");
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("Idempotency-Key header is required");
    }

    @Test
    @DisplayName("Should fail when Idempotency-Key exceeds maximum length")
    void shouldFailWhenKeyTooLong() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        String tooLong = "a".repeat(IdempotencyHeaderFilter.MAX_KEY_LENGTH + 1);
        request.addHeader("Idempotency-Key", tooLong);
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("exceeds maximum length");
    }

    @Test
    @DisplayName("Should bypass validation for GET requests")
    void shouldBypassForGet() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("GET", "/wallets/wallet_1/balance");
        MockHttpServletResponse response = new MockHttpServletResponse();

        filter.doFilter(request, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    @Test
    @DisplayName("Should bypass validation for non-mutating endpoints")
    void shouldBypassForNonIdempotentEndpoints() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/some-other-endpoint");
        MockHttpServletResponse response = new MockHttpServletResponse();

        filter.doFilter(request, response, filterChain);

        verify(filterChain, times(1)).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.OK.value());
    }

    // Path-normalization defense: an exact-string match against /transfers
    // would silently let these three variants slip through and reach the
    // controller without an idempotency key. The fix lower-cases, collapses
    // repeated slashes, and strips a single trailing slash before the lookup.

    @Test
    @DisplayName("Should require key for /transfers/ (trailing slash variant)")
    void shouldRequireKeyForTrailingSlashVariant() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/transfers/");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("Idempotency-Key header is required");
    }

    @Test
    @DisplayName("Should require key for /Transfers (case-only variant)")
    void shouldRequireKeyForCaseVariant() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "/Transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("Idempotency-Key header is required");
    }

    @Test
    @DisplayName("Should require key for //transfers (collapsed double-slash)")
    void shouldRequireKeyForDoubleSlashVariant() throws ServletException, IOException {
        MockHttpServletRequest request = new MockHttpServletRequest("POST", "//transfers");
        MockHttpServletResponse response = new MockHttpServletResponse();
        request.setContent(new byte[0]);

        filter.doFilter(request, response, filterChain);

        verify(filterChain, never()).doFilter(any(), any());
        assertThat(response.getStatus()).isEqualTo(HttpStatus.BAD_REQUEST.value());
        assertThat(response.getContentAsString()).contains("Idempotency-Key header is required");
    }
}
