package com.wallettransfer.repository.jdbc;

import com.wallettransfer.domain.EntryType;
import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.repository.LedgerRepository;
import java.util.List;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** PostgreSQL implementation of {@link LedgerRepository}. */
@Repository
class JdbcLedgerRepository implements LedgerRepository {

  private final JdbcClient jdbc;

  JdbcLedgerRepository(JdbcClient jdbc) {
    this.jdbc = jdbc;
  }

  @Override
  public void insert(LedgerEntry... entries) {
    for (LedgerEntry entry : entries) {
      jdbc.sql(
              """
              INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
              VALUES (:id, :transferId, :walletId, :type, :amount, :createdAt)
              """)
          .param("id", entry.id())
          .param("transferId", entry.transferId())
          .param("walletId", entry.walletId())
          .param("type", entry.type().name())
          .param("amount", entry.amount())
          .param("createdAt", Timestamps.toDb(entry.createdAt()))
          .update();
    }
  }

  @Override
  public List<LedgerEntry> findByTransferId(String transferId) {
    return jdbc.sql(
            """
            SELECT id, transfer_id, wallet_id, type, amount, created_at
              FROM ledger_entries
             WHERE transfer_id = :transferId
             ORDER BY CASE type WHEN 'DEBIT' THEN 0 ELSE 1 END
            """)
        .param("transferId", transferId)
        .query(
            (rs, rowNum) ->
                new LedgerEntry(
                    rs.getString("id"),
                    rs.getString("transfer_id"),
                    rs.getString("wallet_id"),
                    EntryType.valueOf(rs.getString("type")),
                    rs.getLong("amount"),
                    Timestamps.read(rs, "created_at")))
        .list();
  }
}
