package com.wallettransfer.repository.jdbc;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.time.Instant;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;

/** Conversions between {@link Instant} and PostgreSQL {@code timestamptz}. */
final class Timestamps {

  private Timestamps() {}

  static OffsetDateTime toDb(Instant instant) {
    return OffsetDateTime.ofInstant(instant, ZoneOffset.UTC);
  }

  static Instant read(ResultSet rs, String column) throws SQLException {
    return rs.getObject(column, OffsetDateTime.class).toInstant();
  }
}
