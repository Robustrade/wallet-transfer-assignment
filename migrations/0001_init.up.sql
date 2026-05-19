-- core wallet table. balance is in minor units (paisa), int64-safe.
-- check keeps it from ever going negative even if app logic has a bug.
create table wallets (
    id text primary key,
    balance bigint not null check (balance >= 0),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- 1 row per transfer attemp. status is the state machine.
-- from <> to and amount > 0 enforced here, not just code.
create table transfers (
    id uuid primary key default gen_random_uuid (),
    from_wallet text not null references wallets (id),
    to_wallet text not null references wallets (id),
    amount bigint not null check (amount > 0),
    status text not null check (
        status in (
            'PENDING',
            'PROCESSED',
            'FAILED'
        )
    ),
    failure_reason text,
    created_at timestamptz not null default now(),
    check (from_wallet <> to_wallet)
);

-- double entry ledger. every processed transfer = 2 rows.
-- fk to transfers means a ledger row can never be orphaned.
create table ledger_entries (
    id bigserial primary key,
    transfer_id uuid not null references transfers (id),
    wallet_id text not null references wallets (id),
    type text not null check (type in ('DEBIT', 'CREDIT')),
    amount bigint not null check (amount > 0),
    created_at timestamptz not null default now()
);

create index idx_ledger_transfer on ledger_entries (transfer_id);

create index idx_ledger_wallet on ledger_entries (wallet_id);

-- idempotency dedupe. pk on the key gives us the exactly-once guarantee.
create table idempotency_records (
    idempotency_key text primary key,
    request_fingerprint text not null,
    status text not null check (
        status in ('PENDING', 'COMPLETED')
    ),
    transfer_id uuid references transfers (id),
    response_status int,
    response_body jsonb,
    created_at timestamptz not null default now()
);

-- seed money so the service is demoable after migrate.
insert into
    wallets (id, balance)
values ('wallet_1', 100000),
    ('wallet_2', 50000),
    ('wallet_3', 0);