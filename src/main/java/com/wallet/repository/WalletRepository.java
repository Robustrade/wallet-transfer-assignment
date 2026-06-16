package com.wallet.repository;

import com.wallet.domain.Wallet;
import org.jooq.DSLContext;
import org.jooq.Record;
import org.springframework.stereotype.Repository;

import java.time.OffsetDateTime;
import java.util.Optional;

import static com.wallet.jooq.Tables.WALLETS;

@Repository
public class WalletRepository {

    private final DSLContext dsl;

    public WalletRepository(DSLContext dsl) {
        this.dsl = dsl;
    }

    public Optional<Wallet> findById(String id) {
        return dsl.selectFrom(WALLETS)
                .where(WALLETS.ID.eq(id))
                .fetchOptional()
                .map(this::mapRecordToWallet);
    }

    /**
     * Existence check via {@code SELECT EXISTS} — does not materialize the row.
     * Uses {@code dsl.fetchExists} to match the idiom in
     * {@link ClientRepository#existsById(String)} so future readers see one
     * shape, not two.
     */
    public boolean existsById(String id) {
        return dsl.fetchExists(
                dsl.selectOne()
                        .from(WALLETS)
                        .where(WALLETS.ID.eq(id))
        );
    }

    /** Pessimistic read with {@code SELECT ... FOR UPDATE}. */
    public Optional<Wallet> findByIdForUpdate(String id) {
        return dsl.selectFrom(WALLETS)
                .where(WALLETS.ID.eq(id))
                .forUpdate()
                .fetchOptional()
                .map(this::mapRecordToWallet);
    }

    /**
     * Persists balance changes. Concurrency safety relies on the caller having
     * already acquired a row lock via {@link #findByIdForUpdate(String)}.
     *
     * <p>{@code updated_at} is left to the database — the
     * {@code trg_update_wallets_updated_at} trigger writes {@code now()} on
     * every UPDATE — so the DB clock is the single source of truth and is
     * immune to inter-instance clock skew. The trigger-assigned value is
     * surfaced via {@code RETURNING} and propagated into the returned domain
     * object.
     *
     * <p>The post-update {@code balance} is also returned so callers
     * (e.g. ledger snapshot capture) can rely on the DB-confirmed value rather
     * than the in-memory field — defensive against any future reordering that
     * would let the in-memory wallet drift from the persisted row.
     *
     * <p>Asserts that exactly one row was updated. A zero count means the wallet
     * row vanished between the {@code SELECT ... FOR UPDATE} and this call —
     * which should not be possible under our current data model (wallets are
     * never deleted), so it indicates a bug or out-of-band tampering and we
     * abort rather than silently lose the balance update.
     */
    public Wallet save(Wallet wallet) {
        Record r = dsl.update(WALLETS)
                .set(WALLETS.BALANCE, wallet.getBalance())
                .where(WALLETS.ID.eq(wallet.getId()))
                .returning(WALLETS.BALANCE, WALLETS.CREATED_AT, WALLETS.UPDATED_AT)
                .fetchOne();

        if (r == null) {
            throw new IllegalStateException(
                    "Expected to update exactly 1 wallet row for id=" + wallet.getId()
                            + " but updated 0");
        }

        Long persistedBalance = r.get(WALLETS.BALANCE);
        OffsetDateTime createdAt = r.get(WALLETS.CREATED_AT);
        OffsetDateTime updatedAt = r.get(WALLETS.UPDATED_AT);
        return new Wallet(
                wallet.getId(),
                persistedBalance,
                createdAt != null ? createdAt.toInstant() : null,
                updatedAt != null ? updatedAt.toInstant() : null
        );
    }

    private Wallet mapRecordToWallet(Record r) {
        if (r == null) return null;
        OffsetDateTime createdAt = r.get(WALLETS.CREATED_AT);
        OffsetDateTime updatedAt = r.get(WALLETS.UPDATED_AT);
        return new Wallet(
                r.get(WALLETS.ID),
                r.get(WALLETS.BALANCE),
                createdAt != null ? createdAt.toInstant() : null,
                updatedAt != null ? updatedAt.toInstant() : null
        );
    }
}
