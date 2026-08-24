from __future__ import annotations

import os
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from wallet_transfer.handlers.transfers import router as transfers_router
from wallet_transfer.repositories.pool import create_pool
from wallet_transfer.repositories.unit_of_work_asyncpg import AsyncpgUnitOfWorkFactory
from wallet_transfer.services.transfer_service import TransferService


def create_app(*, dsn: str | None = None) -> FastAPI:
    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        pool = await create_pool(dsn or os.environ["DATABASE_URL"])
        app.state.transfer_service = TransferService(AsyncpgUnitOfWorkFactory(pool))
        try:
            yield
        finally:
            await pool.close()

    app = FastAPI(title="Wallet Transfer Service", version="1.0.0", lifespan=lifespan)
    app.include_router(transfers_router)

    @app.exception_handler(RequestValidationError)
    async def handle_validation_error(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        # Squash FastAPI's default structured validation-error list into the same
        # {"detail": string} envelope every other error response uses (ErrorResponse,
        # openapi/spec.yaml) — otherwise malformed-body 422s would carry a different, undocumented
        # shape than every other error path, which the OpenAPI drift check would rightly flag.
        message = "; ".join(
            f"{'.'.join(str(part) for part in error['loc'])}: {error['msg']}"
            for error in exc.errors()
        )
        return JSONResponse(status_code=422, content={"detail": message})

    return app
