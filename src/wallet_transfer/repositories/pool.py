from __future__ import annotations

import asyncpg

# CT6 drives 50-100 concurrent attempts against one wallet; max_size is set with real headroom
# above that ceiling rather than exactly matched to it (ADR-0003), so pool exhaustion is a
# deliberate test scenario, not an accident of undersizing.
DEFAULT_MIN_POOL_SIZE = 10
DEFAULT_MAX_POOL_SIZE = 120


async def create_pool(
    dsn: str,
    *,
    min_size: int = DEFAULT_MIN_POOL_SIZE,
    max_size: int = DEFAULT_MAX_POOL_SIZE,
) -> asyncpg.Pool:
    pool = await asyncpg.create_pool(dsn=dsn, min_size=min_size, max_size=max_size)
    assert pool is not None
    return pool
