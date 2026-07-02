package com.wallettransfer.utility;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class LogSanitizerTest {

  @Test
  void shouldReplaceControlCharacters() {
    assertThat(LogSanitizer.sanitize("wallet\r\ninjected")).isEqualTo("wallet__injected");
  }

  @Test
  void shouldReturnEmptyStringForNull() {
    assertThat(LogSanitizer.sanitize(null)).isEmpty();
  }
}
