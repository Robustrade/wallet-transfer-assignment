-- Generic, operation-agnostic idempotency registry. Exactly-once at the API level lives here
-- rather than on any one table, so the same key space guards transfers and future endpoints.
CREATE TABLE idempotency_records (
    -- UUID v7 assigned by the application (Hibernate UuidGenerator.Style.TIME) on insert.
    id              UUID           PRIMARY KEY,
    idempotency_key TEXT           NOT NULL,
    -- Fingerprint of the request; a key replayed with a different payload is rejected
    -- rather than silently served the original result.
    request_hash    TEXT           NOT NULL,
    -- IN_PROGRESS on first sight, flipped to COMPLETED once the response is captured; lets a
    -- concurrent duplicate detect an in-flight request instead of double-executing.
    status          TEXT           NOT NULL,
    -- Id of the resource the request created (e.g. a transfer). Deliberately not a foreign key:
    -- the registry is polymorphic and may reference different tables across operations.
    target_id       UUID,
    -- Cached response, replayed verbatim for duplicate requests once COMPLETED.
    response_status INTEGER,
    response_body   TEXT,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    -- The unique key both enforces exactly-once and backs the duplicate-lookup index.
    CONSTRAINT idempotency_records_key_unique UNIQUE (idempotency_key),
    CONSTRAINT idempotency_records_status_valid CHECK (status IN ('IN_PROGRESS', 'COMPLETED'))
);
