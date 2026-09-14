-- Applied once by Flyway on first app start; skipped on later starts via flyway_schema_history.

CREATE TABLE wallets (
    id          VARCHAR(36) PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    balance     NUMERIC(15, 2) NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    CONSTRAINT wallets_balance_non_negative_ck CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id               UUID PRIMARY KEY,
    idempotency_key  VARCHAR(128) NOT NULL,
    from_wallet      VARCHAR(36) NOT NULL,
    to_wallet        VARCHAR(36) NOT NULL,
    amount           NUMERIC(15, 2) NOT NULL,
    state            VARCHAR(32) NOT NULL,
    failure_reason   VARCHAR(512),
    created_at       TIMESTAMP NOT NULL,
    updated_at       TIMESTAMP NOT NULL,
    CONSTRAINT transfers_idempotency_key_uk UNIQUE (idempotency_key),
    CONSTRAINT transfers_from_wallet_fk FOREIGN KEY (from_wallet) REFERENCES wallets (id),
    CONSTRAINT transfers_to_wallet_fk FOREIGN KEY (to_wallet) REFERENCES wallets (id),
    CONSTRAINT transfers_state_ck CHECK (state IN ('PENDING', 'FAILED', 'PROCESSED'))
);

CREATE TABLE ledger_entries (
    id                 UUID PRIMARY KEY,
    wallet_id          VARCHAR(36) NOT NULL,
    transfer_id        UUID NOT NULL,
    transaction_type   VARCHAR(16) NOT NULL,
    amount             NUMERIC(15, 2) NOT NULL,
    created_at         TIMESTAMP NOT NULL,
    CONSTRAINT ledger_entries_wallet_id_fk FOREIGN KEY (wallet_id) REFERENCES wallets (id),
    CONSTRAINT ledger_entries_transfer_id_fk FOREIGN KEY (transfer_id) REFERENCES transfers (id),
    CONSTRAINT ledger_entries_transaction_type_ck CHECK (transaction_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_transfer_type_uk UNIQUE (transfer_id, transaction_type)
);

CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id);
CREATE INDEX ledger_entries_transfer_id_idx ON ledger_entries (transfer_id);

CREATE TABLE idempotency_records (
    idempotency_key  VARCHAR(128) PRIMARY KEY,
    transfer_id      UUID NOT NULL,
    request_hash     VARCHAR(128) NOT NULL,
    created_at       TIMESTAMP NOT NULL,
    CONSTRAINT idempotency_records_transfer_id_fk FOREIGN KEY (transfer_id) REFERENCES transfers (id)
);
