package com.rajat.wallet.dto;

import java.util.List;
import org.springframework.data.domain.Page;

/**
 * Stable, explicit pagination envelope. Spring Data's {@code Page}/{@code PageImpl} is deliberately
 * not serialized directly — its JSON shape is version-unstable (Spring Boot logs a warning about
 * it) — so we map it to this fixed contract instead.
 */
public record PageResponse<T>(
    List<T> content,
    int page,
    int size,
    long totalElements,
    int totalPages,
    boolean first,
    boolean last) {

  public static <T> PageResponse<T> from(Page<T> page) {
    return new PageResponse<>(
        page.getContent(),
        page.getNumber(),
        page.getSize(),
        page.getTotalElements(),
        page.getTotalPages(),
        page.isFirst(),
        page.isLast());
  }
}
