package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

/**
 * Wallet-to-wallet money transfer. Status is fixed at construction —
 * {@link #processed} for the happy path, {@link #failed} for the audited
 * failure path. Once written, a row is never updated.
 *
 * <p>{@code createdAt}/{@code updatedAt} are <b>null until the row is
 * persisted</b> — they are assigned by Postgres ({@code DEFAULT now()}) and
 * surfaced via the repository's {@code RETURNING} clause. Using the database
 * clock keeps {@code created_at} ordering consistent across multiple service
 * instances even when their wall clocks drift; it also avoids the ambiguity of
 * a row with two times (the app-clock guess and the DB-side default).
 */
public class Transfer {

    private final UUID id;
    private final String fromWalletId;
    private final String toWalletId;
    private final Long amount;
    private final TransferStatus status;
    private final String errorMessage;
    private final Instant createdAt;
    private final Instant updatedAt;

    /** Reconstruction constructor for repository loading and post-INSERT enrichment. */
    public Transfer(UUID id, String fromWalletId, String toWalletId, Long amount, TransferStatus status,
                    String errorMessage, Instant createdAt, Instant updatedAt) {
        this.id = id;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = status;
        this.errorMessage = errorMessage;
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
    }

    private Transfer(String fromWalletId, String toWalletId, Long amount,
                     TransferStatus status, String errorMessage) {
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("Cannot transfer to the same wallet");
        }
        if (amount == null || amount <= 0) {
            throw new IllegalArgumentException("Transfer amount must be positive");
        }
        this.id = UUID.randomUUID();
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = status;
        this.errorMessage = errorMessage;
        // Timestamps stay null until the DB assigns them via RETURNING.
        this.createdAt = null;
        this.updatedAt = null;
    }

    public static Transfer processed(String fromWalletId, String toWalletId, Long amount) {
        return new Transfer(fromWalletId, toWalletId, amount, TransferStatus.PROCESSED, null);
    }

    public static Transfer failed(String fromWalletId, String toWalletId, Long amount, String errorMessage) {
        return new Transfer(fromWalletId, toWalletId, amount, TransferStatus.FAILED, errorMessage);
    }

    public UUID getId() {
        return id;
    }

    public String getFromWalletId() {
        return fromWalletId;
    }

    public String getToWalletId() {
        return toWalletId;
    }

    public Long getAmount() {
        return amount;
    }

    public TransferStatus getStatus() {
        return status;
    }

    public String getErrorMessage() {
        return errorMessage;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }
}
