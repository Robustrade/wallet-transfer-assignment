#!/usr/bin/env python
"""Demo/stress tooling (ADR-0003, ADR-0010) — drives the real running HTTP API with concurrent
load against the seeded demo wallets (db/seed/V9001__seed_demo_wallets.sql), then verifies
balance reconciliation directly against Postgres. Run manually against the docker-compose stack
(`just demo`, then `just simulate`) — not part of automated CI, which proves the same properties
at test-scale via CT1-CT7 against testcontainers instead.

Scenarios: same-wallet fan-in, an opposite-direction pair, and duplicate idempotency-key replay.
"""

from __future__ import annotations

import argparse
import asyncio
import os
import sys
import uuid

import asyncpg
import httpx

# Fixed demo wallet ids from db/seed/V9001__seed_demo_wallets.sql (ADR-0010) — there's no
# wallet-creation endpoint, so this script has nothing else to reference.
WALLET_HUB = uuid.UUID("00000000-0000-7000-8000-000000000001")
WALLET_A = uuid.UUID("00000000-0000-7000-8000-000000000002")
WALLET_B = uuid.UUID("00000000-0000-7000-8000-000000000003")
WALLET_SINK = uuid.UUID("00000000-0000-7000-8000-000000000004")

DEFAULT_DATABASE_URL = "postgresql://wallet:wallet@localhost:5432/wallet_transfer"


async def fetch_balance(pool: asyncpg.Pool, wallet_id: uuid.UUID) -> int:
    async with pool.acquire() as connection:
        balance = await connection.fetchval("SELECT balance FROM wallets WHERE id = $1", wallet_id)
    assert balance is not None, f"wallet {wallet_id} not found — did you run `just demo`?"
    return int(balance)


async def post_transfer(
    client: httpx.AsyncClient, *, key: str, from_id: uuid.UUID, to_id: uuid.UUID, amount: int
) -> httpx.Response:
    return await client.post(
        "/transfers",
        json={
            "idempotencyKey": key,
            "fromWalletId": str(from_id),
            "toWalletId": str(to_id),
            "amount": amount,
        },
    )


async def run_fan_in(client: httpx.AsyncClient, pool: asyncpg.Pool, *, fan_in: int) -> bool:
    print(f"\n=== Same-wallet fan-in: {fan_in} concurrent attempts on WALLET_HUB ===")
    amount = 100
    initial_hub = await fetch_balance(pool, WALLET_HUB)
    initial_sink = await fetch_balance(pool, WALLET_SINK)

    responses = await asyncio.gather(
        *(
            post_transfer(
                client,
                key=f"sim-fan-in-{uuid.uuid4()}",
                from_id=WALLET_HUB,
                to_id=WALLET_SINK,
                amount=amount,
            )
            for _ in range(fan_in)
        )
    )

    unexpected = [r for r in responses if r.status_code != 200]
    processed = sum(
        1 for r in responses if r.status_code == 200 and r.json()["status"] == "PROCESSED"
    )
    failed = sum(1 for r in responses if r.status_code == 200 and r.json()["status"] == "FAILED")

    final_hub = await fetch_balance(pool, WALLET_HUB)
    final_sink = await fetch_balance(pool, WALLET_SINK)
    expected_hub = initial_hub - processed * amount

    ok = (
        not unexpected
        and final_hub == expected_hub
        and final_hub >= 0
        and final_sink == initial_sink + processed * amount
    )

    print(f"  processed={processed} failed={failed} unexpected_status_codes={len(unexpected)}")
    print(f"  WALLET_HUB balance: {initial_hub} -> {final_hub} (expected {expected_hub})")
    print(f"  WALLET_SINK balance: {initial_sink} -> {final_sink}")
    print(f"  {'PASS' if ok else 'FAIL'}")
    return ok


async def run_opposite_direction_pair(client: httpx.AsyncClient, pool: asyncpg.Pool) -> bool:
    print("\n=== Opposite-direction pair: WALLET_A <-> WALLET_B concurrently ===")
    initial_a = await fetch_balance(pool, WALLET_A)
    initial_b = await fetch_balance(pool, WALLET_B)

    response_ab, response_ba = await asyncio.wait_for(
        asyncio.gather(
            post_transfer(
                client,
                key=f"sim-pair-ab-{uuid.uuid4()}",
                from_id=WALLET_A,
                to_id=WALLET_B,
                amount=100,
            ),
            post_transfer(
                client,
                key=f"sim-pair-ba-{uuid.uuid4()}",
                from_id=WALLET_B,
                to_id=WALLET_A,
                amount=50,
            ),
        ),
        timeout=10.0,
    )

    final_a = await fetch_balance(pool, WALLET_A)
    final_b = await fetch_balance(pool, WALLET_B)
    expected_a = initial_a - 100 + 50
    expected_b = initial_b - 50 + 100

    ok = (
        response_ab.status_code == 200
        and response_ba.status_code == 200
        and response_ab.json()["status"] == "PROCESSED"
        and response_ba.json()["status"] == "PROCESSED"
        and final_a == expected_a
        and final_b == expected_b
    )

    print(f"  WALLET_A balance: {initial_a} -> {final_a} (expected {expected_a})")
    print(f"  WALLET_B balance: {initial_b} -> {final_b} (expected {expected_b})")
    print(f"  {'PASS' if ok else 'FAIL'}")
    return ok


async def run_duplicate_key_replay(client: httpx.AsyncClient, *, fan_in: int) -> bool:
    print(f"\n=== Duplicate idempotency key: {fan_in} concurrent identical requests ===")
    key = f"sim-duplicate-{uuid.uuid4()}"

    responses = await asyncio.gather(
        *(
            post_transfer(client, key=key, from_id=WALLET_A, to_id=WALLET_B, amount=10)
            for _ in range(fan_in)
        )
    )

    bodies = [r.json() for r in responses if r.status_code == 200]
    transfer_ids = {body["id"] for body in bodies}

    ok = all(r.status_code == 200 for r in responses) and len(transfer_ids) == 1

    print(f"  responses: {len(responses)}, distinct transfer ids created: {len(transfer_ids)}")
    print(f"  {'PASS' if ok else 'FAIL'}")
    return ok


async def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-url", default=os.environ.get("SIMULATE_BASE_URL", "http://localhost:8000")
    )
    parser.add_argument(
        "--database-url", default=os.environ.get("DATABASE_URL", DEFAULT_DATABASE_URL)
    )
    parser.add_argument(
        "--fan-in",
        type=int,
        default=100,
        help="concurrent attempts for the fan-in/duplicate-key scenarios",
    )
    args = parser.parse_args()

    pool = await asyncpg.create_pool(dsn=args.database_url, min_size=2, max_size=args.fan_in + 10)
    limits = httpx.Limits(max_connections=args.fan_in + 20)
    async with httpx.AsyncClient(base_url=args.base_url, timeout=30.0, limits=limits) as client:
        try:
            results = [
                await run_fan_in(client, pool, fan_in=args.fan_in),
                await run_opposite_direction_pair(client, pool),
                await run_duplicate_key_replay(client, fan_in=min(args.fan_in, 25)),
            ]
        finally:
            await pool.close()

    ok = all(results)
    print("\nRESULT:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
