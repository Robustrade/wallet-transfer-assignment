package com.wallet.repository;

import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;
import org.jooq.DSLContext;
import org.jooq.Record;
import org.jooq.impl.DSL;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.time.OffsetDateTime;
import java.util.List;
import java.util.UUID;

import static com.wallet.jooq.Tables.LEDGER_ENTRIES;

@Repository
public class LedgerEntryRepository {

    private final DSLContext dsl;

    public LedgerEntryRepository(DSLContext dsl) {
        this.dsl = dsl;
    }

    public List<LedgerEntry> findByTransferId(UUID transferId) {
        return dsl.selectFrom(LEDGER_ENTRIES)
                .where(LEDGER_ENTRIES.TRANSFER_ID.eq(transferId))
                .fetch()
                .map(this::mapRecordToLedgerEntry);
    }

    public List<LedgerEntry> findByWalletId(String walletId) {
        return dsl.selectFrom(LEDGER_ENTRIES)
                .where(LEDGER_ENTRIES.WALLET_ID.eq(walletId))
                .fetch()
                .map(this::mapRecordToLedgerEntry);
    }

    /**
     * AUDIT-ONLY: full-table scan over {@code ledger_entries} — net sum across
     * the entire ledger (credits − debits); should always be {@code 0} as a
     * double-entry invariant.
     *
     * <p><strong>DO NOT call from request-path code.</strong> This is an O(N)
     * scan with no WHERE clause. As ledger volume grows it will degrade any
     * caller it touches. Today it is exercised exclusively by the
     * ledger-balance integration test and ad-hoc reconciliation tooling. If a
     * production audit dashboard ever needs this number, route it through a
     * background job (or an incrementally maintained materialized view) — not
     * through synchronous request handling.
     */
    public Long computeNetLedgerBalance() {
        return dsl.select(
                DSL.coalesce(
                        DSL.sum(
                                DSL.case_()
                                        .when(LEDGER_ENTRIES.ENTRY_TYPE.eq("CREDIT"), LEDGER_ENTRIES.AMOUNT)
                                        .else_(LEDGER_ENTRIES.AMOUNT.neg())
                        ),
                        BigDecimal.ZERO
                )
        )
        .from(LEDGER_ENTRIES)
        .fetchOneInto(Long.class);
    }

    /**
     * Inserts the row without an app-clock timestamp — Postgres assigns
     * {@code created_at} via {@code DEFAULT now()} and the {@code RETURNING}
     * clause surfaces it. The DB clock is the single source of truth for
     * ledger ordering, immune to inter-instance clock skew.
     */
    public LedgerEntry save(LedgerEntry entry) {
        Record r = dsl.insertInto(LEDGER_ENTRIES)
                .set(LEDGER_ENTRIES.ID, entry.getId())
                .set(LEDGER_ENTRIES.WALLET_ID, entry.getWalletId())
                .set(LEDGER_ENTRIES.TRANSFER_ID, entry.getTransferId())
                .set(LEDGER_ENTRIES.ENTRY_TYPE, entry.getEntryType().name())
                .set(LEDGER_ENTRIES.AMOUNT, entry.getAmount())
                .set(LEDGER_ENTRIES.RUNNING_BALANCE, entry.getRunningBalance())
                .returning(LEDGER_ENTRIES.CREATED_AT)
                .fetchOne();

        if (r == null) {
            throw new IllegalStateException(
                    "Ledger entry insert returned no row for id=" + entry.getId());
        }

        OffsetDateTime createdAt = r.get(LEDGER_ENTRIES.CREATED_AT);
        return new LedgerEntry(
                entry.getId(),
                entry.getWalletId(),
                entry.getTransferId(),
                entry.getEntryType(),
                entry.getAmount(),
                entry.getRunningBalance(),
                createdAt != null ? createdAt.toInstant() : null
        );
    }

    private LedgerEntry mapRecordToLedgerEntry(Record r) {
        if (r == null) return null;
        OffsetDateTime createdAt = r.get(LEDGER_ENTRIES.CREATED_AT);
        return new LedgerEntry(
                r.get(LEDGER_ENTRIES.ID),
                r.get(LEDGER_ENTRIES.WALLET_ID),
                r.get(LEDGER_ENTRIES.TRANSFER_ID),
                EntryType.valueOf(r.get(LEDGER_ENTRIES.ENTRY_TYPE)),
                r.get(LEDGER_ENTRIES.AMOUNT),
                r.get(LEDGER_ENTRIES.RUNNING_BALANCE),
                createdAt != null ? createdAt.toInstant() : null
        );
    }
}
