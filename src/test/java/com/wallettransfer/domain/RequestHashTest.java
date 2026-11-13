package com.wallettransfer.domain;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class RequestHashTest {

  @Test
  void isDeterministic() {
    assertThat(RequestHash.of("a", "b", 100)).isEqualTo(RequestHash.of("a", "b", 100));
  }

  @Test
  void changesWhenAnyBusinessFieldChanges() {
    String base = RequestHash.of("a", "b", 100);

    assertThat(RequestHash.of("a", "b", 101)).isNotEqualTo(base);
    assertThat(RequestHash.of("b", "a", 100)).isNotEqualTo(base);
    assertThat(RequestHash.of("a", "c", 100)).isNotEqualTo(base);
  }

  @Test
  void isNotFooledByConcatenation() {
    assertThat(RequestHash.of("ab", "c", 1)).isNotEqualTo(RequestHash.of("a", "bc", 1));
  }

  @Test
  void isHexEncodedSha256() {
    assertThat(RequestHash.of("a", "b", 1)).matches("[0-9a-f]{64}");
  }
}
