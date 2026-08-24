from __future__ import annotations

from uuid import uuid4

import asyncpg
from fastapi.testclient import TestClient

from tests.repository.helpers import seed_wallet


async def test_e1_normal_transfer_returns_200_processed(
    client: TestClient, pool: asyncpg.Pool
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)

    response = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-normal",
            "fromWalletId": str(from_wallet.id),
            "toWalletId": str(to_wallet.id),
            "amount": 250,
        },
    )

    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "PROCESSED"
    assert body["failureReason"] is None
    assert body["fromWalletId"] == str(from_wallet.id)
    assert body["toWalletId"] == str(to_wallet.id)
    assert body["amount"] == 250
    assert body["idempotencyKey"] == "e1-normal"


async def test_e1_sequential_idempotent_replay_returns_identical_result(
    client: TestClient, pool: asyncpg.Pool
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)
    payload = {
        "idempotencyKey": "e1-replay",
        "fromWalletId": str(from_wallet.id),
        "toWalletId": str(to_wallet.id),
        "amount": 100,
    }

    first = client.post("/transfers", json=payload)
    second = client.post("/transfers", json=payload)

    assert first.status_code == 200
    assert second.status_code == 200
    assert first.json() == second.json()


async def test_e1_malformed_body_returns_422(client: TestClient) -> None:
    response = client.post(
        "/transfers",
        json={"idempotencyKey": "e1-malformed", "fromWalletId": str(uuid4())},
    )

    assert response.status_code == 422
    assert "detail" in response.json()


async def test_e1_self_transfer_returns_422(client: TestClient, pool: asyncpg.Pool) -> None:
    wallet = await seed_wallet(pool, balance=1000)

    response = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-self",
            "fromWalletId": str(wallet.id),
            "toWalletId": str(wallet.id),
            "amount": 100,
        },
    )

    assert response.status_code == 422


async def test_e1_non_positive_amount_returns_422(client: TestClient, pool: asyncpg.Pool) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)

    response = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-non-positive",
            "fromWalletId": str(from_wallet.id),
            "toWalletId": str(to_wallet.id),
            "amount": 0,
        },
    )

    assert response.status_code == 422


async def test_e1_nonexistent_wallet_returns_404(client: TestClient, pool: asyncpg.Pool) -> None:
    to_wallet = await seed_wallet(pool, balance=0)

    response = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-404",
            "fromWalletId": str(uuid4()),
            "toWalletId": str(to_wallet.id),
            "amount": 100,
        },
    )

    assert response.status_code == 404


async def test_e1_reused_key_different_payload_returns_409(
    client: TestClient, pool: asyncpg.Pool
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)

    first = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-409",
            "fromWalletId": str(from_wallet.id),
            "toWalletId": str(to_wallet.id),
            "amount": 100,
        },
    )
    second = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-409",
            "fromWalletId": str(from_wallet.id),
            "toWalletId": str(to_wallet.id),
            "amount": 200,
        },
    )

    assert first.status_code == 200
    assert second.status_code == 409


async def test_e1_insufficient_funds_returns_200_failed(
    client: TestClient, pool: asyncpg.Pool
) -> None:
    from_wallet = await seed_wallet(pool, balance=10)
    to_wallet = await seed_wallet(pool, balance=0)

    response = client.post(
        "/transfers",
        json={
            "idempotencyKey": "e1-insufficient",
            "fromWalletId": str(from_wallet.id),
            "toWalletId": str(to_wallet.id),
            "amount": 100,
        },
    )

    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "FAILED"
    assert body["failureReason"] == "INSUFFICIENT_FUNDS"
