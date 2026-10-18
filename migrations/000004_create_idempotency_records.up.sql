CREATE TABLE idempotency_records (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    idempotency_key VARCHAR(64) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,

    transfer_id BIGINT,

    response_status INT,
    response_body JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_idempotency_key
        UNIQUE (idempotency_key),

    CONSTRAINT chk_idempotency_key_length
        CHECK (LENGTH(idempotency_key) BETWEEN 8 AND 64),

    CONSTRAINT fk_idempotency_transfer
        FOREIGN KEY (transfer_id) REFERENCES transfers(id),

    CONSTRAINT chk_response_status
        CHECK (response_status BETWEEN 100 AND 599),

    CONSTRAINT chk_response_complete
        CHECK (
            (response_status IS NULL AND response_body IS NULL)
            OR
            (response_status IS NOT NULL AND response_body IS NOT NULL)
        )
);