package com.wallet.repository;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import org.jooq.DSLContext;
import org.jooq.Record;
import org.springframework.stereotype.Repository;

import java.time.OffsetDateTime;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

import static com.wallet.jooq.Tables.TRANSFERS;

/**
 * Persistence for {@link Transfer}. Insert-only — terminal state is fixed at
 * write time, so the table itself enforces append-only history.
 */
@Repository
public class TransferRepository {

    private final DSLContext dsl;

    public TransferRepository(DSLContext dsl) {
        this.dsl = dsl;
    }

    public Optional<Transfer> findById(UUID id) {
        return dsl.selectFrom(TRANSFERS)
                .where(TRANSFERS.ID.eq(id))
                .fetchOptional()
                .map(this::mapRecordToTransfer);
    }

    /**
     * Returns the most recent transfers where the wallet is sender or receiver,
     * ordered by created_at desc and capped at {@code limit}.
     *
     * <p>Implemented as {@code UNION ALL} of two indexed, individually limited
     * sub-queries. A single {@code WHERE from = ? OR to = ?} predicate cannot
     * use both {@code (from_wallet_id, created_at)} and
     * {@code (to_wallet_id, created_at)} indexes — Postgres falls back to a
     * sequential scan or BitmapOr — so we ask for each side separately, take
     * up to {@code limit} from each, and merge in memory. Each sub-query hits
     * its index directly; the outer sort is over at most {@code 2 * limit}
     * rows. Duplicate elimination is unnecessary because a transfer cannot
     * have {@code from_wallet_id = to_wallet_id} (CHECK constraint).
     */
    public List<Transfer> findByWalletId(String walletId, int limit) {
        var sentLeg = dsl.selectFrom(TRANSFERS)
                .where(TRANSFERS.FROM_WALLET_ID.eq(walletId))
                .orderBy(TRANSFERS.CREATED_AT.desc())
                .limit(limit);
        var receivedLeg = dsl.selectFrom(TRANSFERS)
                .where(TRANSFERS.TO_WALLET_ID.eq(walletId))
                .orderBy(TRANSFERS.CREATED_AT.desc())
                .limit(limit);

        // Wrap the union in an explicit derived table. Without this, jOOQ generates
        // an outer ORDER BY "transfers"."created_at" — but the inner table is no
        // longer in scope at the outer level, so Postgres throws BadSqlGrammar.
        // The alias makes the merged column reachable at the outer scope.
        var merged = sentLeg.unionAll(receivedLeg).asTable("merged");
        return dsl.selectFrom(merged)
                .orderBy(merged.field(TRANSFERS.CREATED_AT).desc())
                .limit(limit)
                .fetch()
                .map(this::mapRecordToTransfer);
    }

    /**
     * Inserts the row without app-clock timestamps — Postgres assigns
     * {@code created_at}/{@code updated_at} via {@code DEFAULT now()} and the
     * {@code RETURNING} clause surfaces them. The DB clock is the single
     * source of truth for ordering, immune to inter-instance clock skew.
     */
    public Transfer insert(Transfer transfer) {
        Record r = dsl.insertInto(TRANSFERS)
                .set(TRANSFERS.ID, transfer.getId())
                .set(TRANSFERS.FROM_WALLET_ID, transfer.getFromWalletId())
                .set(TRANSFERS.TO_WALLET_ID, transfer.getToWalletId())
                .set(TRANSFERS.AMOUNT, transfer.getAmount())
                .set(TRANSFERS.STATUS, transfer.getStatus().name())
                .set(TRANSFERS.ERROR_MESSAGE, transfer.getErrorMessage())
                .returning(TRANSFERS.CREATED_AT, TRANSFERS.UPDATED_AT)
                .fetchOne();

        if (r == null) {
            throw new IllegalStateException(
                    "Transfer insert returned no row for id=" + transfer.getId());
        }

        OffsetDateTime createdAt = r.get(TRANSFERS.CREATED_AT);
        OffsetDateTime updatedAt = r.get(TRANSFERS.UPDATED_AT);
        return new Transfer(
                transfer.getId(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getErrorMessage(),
                createdAt != null ? createdAt.toInstant() : null,
                updatedAt != null ? updatedAt.toInstant() : null
        );
    }

    private Transfer mapRecordToTransfer(Record r) {
        if (r == null) return null;
        OffsetDateTime createdAt = r.get(TRANSFERS.CREATED_AT);
        OffsetDateTime updatedAt = r.get(TRANSFERS.UPDATED_AT);
        return new Transfer(
                r.get(TRANSFERS.ID),
                r.get(TRANSFERS.FROM_WALLET_ID),
                r.get(TRANSFERS.TO_WALLET_ID),
                r.get(TRANSFERS.AMOUNT),
                TransferStatus.valueOf(r.get(TRANSFERS.STATUS)),
                r.get(TRANSFERS.ERROR_MESSAGE),
                createdAt != null ? createdAt.toInstant() : null,
                updatedAt != null ? updatedAt.toInstant() : null
        );
    }
}
