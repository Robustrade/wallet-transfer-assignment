-- Each wallet holds a balance in one currency and starts with a zero balance.
--
-- We use TEXT for IDs so values such as 'wallet_1' can be supplied by the app.
-- Currency codes also use TEXT, which does not add the padding of CHAR(3).
-- Codes must contain three uppercase letters, such as 'INR', 'USD', or 'JPY'.
-- The app must check that the currency is supported and know its decimal places.
-- UNIQUE (id, currency_code) lets transfers reference a wallet and its currency
-- together. Different wallets can still use the same currency, such as INR.
--
-- We use BIGINT for balances to store money exactly as whole minor units.
-- It has room for larger balances than INTEGER and maps to Go's int64.
-- For example, one rupee has 100 paise, so INR 100 is stored as 10000 paise:
--   INR 100    * 100  = 10000  (2 decimal places)
--   INR 10.50  * 100  = 1050
--   USD 100    * 100  = 10000  (2 decimal places)
--   JPY 100    * 1    = 100    (0 decimal places)
--   KWD 100    * 1000 = 100000 (3 decimal places)
-- Balance updates use integer arithmetic to avoid floating-point rounding.
-- A balance cannot be negative; transfers will also need transaction locking.
--
-- We use TIMESTAMPTZ to record the same instant regardless of the time zone.
-- For example, 10:00 UTC and 15:30 in India represent the same instant.
-- PostgreSQL displays the time in the session's time zone. CURRENT_TIMESTAMP
-- supplies the transaction start time from the database's clock.
CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    currency_code TEXT NOT NULL,
    balance_minor BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallets_id_currency_unique
        UNIQUE (id, currency_code),
    CONSTRAINT wallets_currency_code_format
        CHECK (currency_code COLLATE "C" ~ '^[A-Z]{3}$'),
    CONSTRAINT wallets_balance_nonnegative
        CHECK (balance_minor >= 0)
);

-- Each transfer records the source wallet, destination wallet, amount, and
-- outcome.
--
-- Both foreign keys use the transfer's one currency_code value. For an INR
-- transfer, (from_wallet_id, INR) and (to_wallet_id, INR) must both exist in
-- wallets, so an INR-to-USD transfer is rejected by the database.
CREATE TABLE transfers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    from_wallet_id TEXT NOT NULL,
    to_wallet_id   TEXT NOT NULL,
    currency_code  TEXT NOT NULL,

    amount_minor   BIGINT NOT NULL CHECK (amount_minor > 0),

    status         TEXT NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),

    failure_code   TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at   TIMESTAMPTZ,

    CONSTRAINT transfers_from_wallet_currency_fk
        FOREIGN KEY (from_wallet_id, currency_code)
        REFERENCES wallets (id, currency_code)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT transfers_to_wallet_currency_fk
        FOREIGN KEY (to_wallet_id, currency_code)
        REFERENCES wallets (id, currency_code)
        ON UPDATE RESTRICT ON DELETE RESTRICT,

    CHECK (from_wallet_id <> to_wallet_id),

    CHECK (
        (
            status = 'PENDING'
            AND completed_at IS NULL
            AND failure_code IS NULL
        )
        OR
        (
            status = 'PROCESSED'
            AND completed_at IS NOT NULL
            AND failure_code IS NULL
        )
        OR
        (
            status = 'FAILED'
            AND completed_at IS NOT NULL
            AND failure_code IS NOT NULL
        )
    )
);

-- A successful transfer has one DEBIT entry and one CREDIT entry. For example,
-- INR 100 * 100 = 10000 leaves the source and 10000 enters the destination.
-- BIGINT IDENTITY generates each entry's ID; UUID and TEXT references match
-- the transfer and wallet IDs. BIGINT amounts use the transfer's minor units.
-- UNIQUE (transfer_id, entry_type) prevents duplicate debits or credits.
-- The application must still write both entries with the correct wallets and
-- equal amounts, together with the balances and transfer outcome in one
-- transaction. Treat committed entries as permanent history.
CREATE TABLE ledger_entries (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    transfer_id  UUID NOT NULL
                 REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id    TEXT NOT NULL
                 REFERENCES wallets(id) ON DELETE RESTRICT,

    entry_type   TEXT NOT NULL
                 CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (transfer_id, entry_type)
);

-- Save the request and its response so a retry with the same key, such as
-- 'abc123', returns the original result without moving money again.
-- TEXT accepts client-generated keys. JSONB stores normalized request fields
-- for comparison without depending on JSON object key order or whitespace.
-- SMALLINT has enough range for HTTP status codes; TEXT preserves the response
-- body's exact serialized JSON. The transfer reference is optional so a saved
-- rejection, such as a missing wallet, can have a response without a transfer.
-- Both response fields may be NULL while the transaction claims the key.
-- The application must fill both before committing and save them in the same
-- transaction as the transfer, balances, and ledger entries.
CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY
                    CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),

    request_payload JSONB NOT NULL,

    transfer_id     UUID UNIQUE
                    REFERENCES transfers(id) ON DELETE RESTRICT,

    http_status     SMALLINT
                    CHECK (http_status BETWEEN 200 AND 599),
    response_body   TEXT,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (
        (http_status IS NULL AND response_body IS NULL)
        OR
        (http_status IS NOT NULL AND response_body IS NOT NULL)
    )
);

CREATE INDEX transfers_from_wallet_idx
    ON transfers (from_wallet_id, created_at DESC);

CREATE INDEX transfers_to_wallet_idx
    ON transfers (to_wallet_id, created_at DESC);

CREATE INDEX ledger_entries_wallet_idx
    ON ledger_entries (wallet_id, created_at DESC, id DESC);
