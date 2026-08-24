"""Shared fixtures for the repository/integration and end-to-end tiers (ADR-0006): one
session-scoped Postgres testcontainers instance with Flyway migrations applied once, not a
fresh container per test — CT1-CT7 each already run multiple concurrent operations, and
starting a container per test would be too slow to sustain that.

Flyway runs as its own container (the same official flyway/flyway image `just migrate` uses,
not a different mechanism) attached to a shared Docker network alongside the Postgres
testcontainer, reachable by network alias — this is "the same migrations applied the same way"
ADR-0004 asks for, not a from-scratch schema-creation path that could drift from it."""

from __future__ import annotations

import subprocess
from collections.abc import AsyncIterator, Iterator
from pathlib import Path

import asyncpg
import pytest
from testcontainers.core.network import Network
from testcontainers.postgres import PostgresContainer

from wallet_transfer.repositories.pool import create_pool
from wallet_transfer.repositories.unit_of_work_asyncpg import AsyncpgUnitOfWorkFactory

REPO_ROOT = Path(__file__).resolve().parent.parent
FLYWAY_IMAGE = "flyway/flyway:10-alpine"
POSTGRES_USER = "wallet"
POSTGRES_PASSWORD = "wallet"
POSTGRES_DB = "wallet_transfer"


@pytest.fixture(scope="session")
def postgres_network() -> Iterator[Network]:
    network = Network()
    network.create()
    try:
        yield network
    finally:
        network.remove()


@pytest.fixture(scope="session")
def postgres_container(postgres_network: Network) -> Iterator[PostgresContainer]:
    container = (
        PostgresContainer(
            "postgres:16-alpine",
            username=POSTGRES_USER,
            password=POSTGRES_PASSWORD,
            dbname=POSTGRES_DB,
        )
        .with_network(postgres_network)
        .with_network_aliases("postgres")
    )
    with container:
        _apply_migrations(postgres_network.name)
        yield container


def _apply_migrations(network_name: str) -> None:
    result = subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--network",
            network_name,
            "-v",
            f"{REPO_ROOT}/db/migrations:/flyway/sql:ro",
            FLYWAY_IMAGE,
            "-url=jdbc:postgresql://postgres:5432/wallet_transfer",
            f"-user={POSTGRES_USER}",
            f"-password={POSTGRES_PASSWORD}",
            "-connectRetries=10",
            "migrate",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(f"flyway migrate failed:\n{result.stdout}\n{result.stderr}")


@pytest.fixture
def postgres_dsn(postgres_container: PostgresContainer) -> str:
    return (
        f"postgresql://{POSTGRES_USER}:{POSTGRES_PASSWORD}@"
        f"{postgres_container.get_container_host_ip()}:"
        f"{postgres_container.get_exposed_port(5432)}/{POSTGRES_DB}"
    )


@pytest.fixture
async def pool(postgres_dsn: str) -> AsyncIterator[asyncpg.Pool]:
    # Function-scoped, not session-scoped: an asyncpg pool is bound to the event loop it was
    # created on, and pytest-asyncio gives each test function its own loop by default. Recreating
    # the pool per test keeps everything on one loop and sidesteps cross-loop errors entirely —
    # the container itself (session-scoped, no event loop involved) is what's actually expensive
    # to keep, not the pool.
    db_pool = await create_pool(postgres_dsn, min_size=2, max_size=120)
    try:
        yield db_pool
    finally:
        await db_pool.close()


@pytest.fixture
async def clean_db(pool: asyncpg.Pool) -> AsyncIterator[None]:
    """Function-scoped TRUNCATE before each test, not a transaction-rollback-per-test pattern
    (ADR-0006) — the code under test issues real commits (locking and idempotency both depend on
    commit visibility across separate connections), so nesting the test in an outer transaction
    would mask exactly what's being verified."""
    async with pool.acquire() as connection:
        await connection.execute(
            "TRUNCATE wallets, transfers, ledger_entries, idempotency_records CASCADE"
        )
    yield


@pytest.fixture
def uow_factory(pool: asyncpg.Pool) -> AsyncpgUnitOfWorkFactory:
    return AsyncpgUnitOfWorkFactory(pool)
