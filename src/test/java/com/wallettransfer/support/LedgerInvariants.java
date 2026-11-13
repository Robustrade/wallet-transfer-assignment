package com.wallettransfer.support;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.Map;
import org.springframework.jdbc.core.JdbcTemplate;

/**
 * The accounting rules that must hold at all times, checked directly against the database:
 *
 * <ol>
 *   <li>total DEBITs equal total CREDITs (the ledger balances)
 *   <li>every wallet balance equals opening balance + credits - debits, and is never negative
 *   <li>every PROCESSED transfer has exactly two entries; other transfers have none
 * </ol>
 */
public final class LedgerInvariants {

  private LedgerInvariants() {}

  public static void assertHold(JdbcTemplate jdbc) {
    Map<String, Object> totals =
        jdbc.queryForMap(
            """
            SELECT COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0)::bigint  AS debits,
                   COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)::bigint AS credits
              FROM ledger_entries
            """);
    assertThat(totals.get("debits"))
        .as("ledger must balance: total debits == total credits")
        .isEqualTo(totals.get("credits"));

    List<Map<String, Object>> wallets =
        jdbc.queryForList(
            """
            SELECT w.id, w.balance,
                   (w.opening_balance
                   + COALESCE((SELECT SUM(amount) FROM ledger_entries l
                                WHERE l.wallet_id = w.id AND l.type = 'CREDIT'), 0)
                   - COALESCE((SELECT SUM(amount) FROM ledger_entries l
                                WHERE l.wallet_id = w.id AND l.type = 'DEBIT'), 0))::bigint
                   AS derived
              FROM wallets w
            """);
    for (Map<String, Object> wallet : wallets) {
      assertThat(wallet.get("balance"))
          .as("wallet %s: stored balance must equal opening + credits - debits", wallet.get("id"))
          .isEqualTo(wallet.get("derived"));
      assertThat((Long) wallet.get("balance")).isNotNegative();
    }

    List<Map<String, Object>> mismatched =
        jdbc.queryForList(
            """
            SELECT t.id, t.state,
                   (SELECT COUNT(*) FROM ledger_entries l WHERE l.transfer_id = t.id) AS entries
              FROM transfers t
             WHERE (t.state = 'PROCESSED'
                    AND (SELECT COUNT(*) FROM ledger_entries l WHERE l.transfer_id = t.id) <> 2)
                OR (t.state <> 'PROCESSED'
                    AND EXISTS (SELECT 1 FROM ledger_entries l WHERE l.transfer_id = t.id))
            """);
    assertThat(mismatched).as("transfers whose ledger entries do not match their state").isEmpty();
  }
}
