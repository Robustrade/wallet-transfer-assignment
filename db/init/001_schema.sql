-- Schema for the wallet-transfer service.
-- Auto-applied by the official postgres image on first container start
-- (files under /docker-entrypoint-initdb.d run once, when the data
-- directory is empty). To re-apply after an edit, drop the compose
-- volume: `docker compose down -v`.

CREATE TYPE ledger_entry_type AS ENUM (
    'DEBIT',
    'CREDIT'
);

CREATE TYPE transfer_state AS ENUM (
    'PENDING',
    'PROCESSED',
    'FAILED'
);

CREATE TABLE wallets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_name text NOT NULL,
    balance bigint DEFAULT 0 NOT NULL,
    version integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT wallets_pkey PRIMARY KEY (id),
    CONSTRAINT wallets_balance_check CHECK ((balance >= 0))
);

CREATE TABLE transfers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    from_wallet_id uuid NOT NULL,
    to_wallet_id uuid NOT NULL,
    amount bigint NOT NULL,
    state transfer_state DEFAULT 'PENDING'::transfer_state NOT NULL,
    failure_reason text,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT transfers_pkey PRIMARY KEY (id),
    CONSTRAINT chk_distinct_wallets CHECK ((from_wallet_id <> to_wallet_id)),
    CONSTRAINT transfers_amount_check CHECK ((amount > 0)),
    CONSTRAINT transfers_from_wallet_id_fkey FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),
    CONSTRAINT transfers_to_wallet_id_fkey FOREIGN KEY (to_wallet_id) REFERENCES wallets(id)
);

-- Backs both idempotency-key lookups and the unique-constraint race that
-- transferRepo.Create()/isUniqueViolation() rely on to detect two
-- concurrent requests submitted with the same idempotency key.
CREATE UNIQUE INDEX uq_transfers_idempotency_key ON transfers USING btree (idempotency_key);

CREATE TABLE ledger_entries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    transfer_id uuid NOT NULL,
    wallet_id uuid NOT NULL,
    type ledger_entry_type NOT NULL,
    amount bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ledger_entries_pkey PRIMARY KEY (id),
    CONSTRAINT ledger_entries_amount_check CHECK ((amount > 0)),
    CONSTRAINT ledger_entries_transfer_id_fkey FOREIGN KEY (transfer_id) REFERENCES transfers(id),
    CONSTRAINT ledger_entries_wallet_id_fkey FOREIGN KEY (wallet_id) REFERENCES wallets(id),
    CONSTRAINT uq_ledger_transfer_wallet_type UNIQUE (transfer_id, wallet_id, type)
);

-- Durable idempotency store: caches the full outcome (status code + response
-- body) of a request so a retried request with the same key can be replayed
-- without re-running the transfer.
CREATE TABLE idempotency_records (
    idempotency_key text NOT NULL,
    request_hash text NOT NULL,
    transfer_id uuid,
    response_body jsonb,
    status_code integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT idempotency_records_pkey PRIMARY KEY (idempotency_key),
    CONSTRAINT idempotency_records_transfer_id_fkey FOREIGN KEY (transfer_id) REFERENCES transfers(id)
);
