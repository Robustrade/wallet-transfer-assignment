-- Wallet transfer service: initial schema.
--
-- Design notes that the DDL below encodes:
--   * Money is BIGINT in minor units. Never a float, anywhere.
--   * A wallet cannot store a negative balance, so an overdraft is impossible
--     to persist rather than merely prevented by application logic. The service
--     still checks sufficiency under the row lock; this CHECK is the backstop
--     (a violation aborts the transaction, so it cannot be the primary path).
--   * transfers.idempotency_key is UNIQUE. That constraint, not application
--     code, is what arbitrates concurrent requests carrying the same key.
--   * A transfer produces exactly two ledger entries, one DEBIT and one CREDIT.
--     UNIQUE (transfer_id, type) stops a second of either side; the pair being
--     complete and balanced is held by the domain, not by the schema.

CREATE TABLE wallets (
    id         TEXT        PRIMARY KEY,
    balance    BIGINT      NOT NULL CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              TEXT        PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    -- Fingerprint of the request parameters this key was first used with, so a
    -- key replayed with different parameters can be rejected rather than
    -- silently answered with an unrelated transfer.
    request_hash    TEXT        NOT NULL,
    from_wallet_id  TEXT        NOT NULL,
    to_wallet_id    TEXT        NOT NULL,
    amount          BIGINT      NOT NULL CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    state           TEXT        NOT NULL CONSTRAINT transfers_state_valid CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),

    -- Named explicitly: the handler maps these two violations to a 404 naming
    -- the offending wallet, so the names are part of the contract.
    CONSTRAINT transfers_from_wallet_fk FOREIGN KEY (from_wallet_id) REFERENCES wallets (id),
    CONSTRAINT transfers_to_wallet_fk   FOREIGN KEY (to_wallet_id)   REFERENCES wallets (id),

    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),

    -- A failure always carries a reason; a non-failure never does. Blank counts
    -- as absent: '' is not NULL, so without the trim a FAILED transfer could
    -- satisfy this while explaining nothing.
    CONSTRAINT transfers_failure_reason_matches_state CHECK (
        (state = 'FAILED') = (failure_reason IS NOT NULL AND btrim(failure_reason) <> '')
    )
);

CREATE TABLE ledger_entries (
    id          BIGSERIAL   PRIMARY KEY,
    transfer_id TEXT        NOT NULL CONSTRAINT ledger_entries_transfer_fk REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL CONSTRAINT ledger_entries_wallet_fk   REFERENCES wallets (id),
    type        TEXT        NOT NULL CONSTRAINT ledger_entries_type_valid  CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- At most one DEBIT and at most one CREDIT per transfer. It does not
    -- require both to exist, nor that their amounts agree with the transfer:
    -- that is held by domain.LedgerEntriesFor, which builds the pair from one
    -- transfer. See "The ledger invariant" in docs/design.md. Also serves as the
    -- transfer_id-leading index for reading a transfer's entries.
    CONSTRAINT ledger_entries_one_per_transfer_side UNIQUE (transfer_id, type)
);

-- Per-wallet statement reads, and the test that asserts stored balance equals
-- the sum of a wallet's ledger entries. This is the only secondary index: the
-- transfers table is reached by primary key or by idempotency_key, both already
-- backed by unique indexes, and no read path yet scans by wallet. See
-- docs/design.md for what a transfer-history endpoint would add.
CREATE INDEX idx_ledger_entries_wallet ON ledger_entries (wallet_id, id);
