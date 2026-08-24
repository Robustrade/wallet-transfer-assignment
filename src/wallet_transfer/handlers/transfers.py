from __future__ import annotations

from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Request

from wallet_transfer.domain.transfer import InvalidTransferError, Transfer
from wallet_transfer.handlers.generated_models import (
    CreateTransferRequest as CreateTransferRequestBody,
)
from wallet_transfer.handlers.generated_models import (
    ErrorResponse,
    Status,
    TransferResponse,
)
from wallet_transfer.services.errors import (
    IdempotencyKeyReusedError,
    RetryExhaustedError,
    WalletNotFoundError,
)
from wallet_transfer.services.transfer_service import CreateTransferRequest, TransferService

router = APIRouter()


def get_transfer_service(request: Request) -> TransferService:
    service = request.app.state.transfer_service
    assert isinstance(service, TransferService)
    return service


@router.post(
    "/transfers",
    operation_id="createTransfer",
    response_model=TransferResponse,
    responses={
        404: {"model": ErrorResponse, "description": "Wallet not found"},
        409: {
            "model": ErrorResponse,
            "description": "Idempotency key reused for a different request",
        },
        422: {"model": ErrorResponse, "description": "Malformed body or an invalid transfer shape"},
        503: {
            "model": ErrorResponse,
            "description": "Bounded retry exhausted; safe to retry with the same key",
        },
    },
)
async def create_transfer(
    body: CreateTransferRequestBody,
    service: Annotated[TransferService, Depends(get_transfer_service)],
) -> TransferResponse:
    try:
        transfer = await service.create_transfer(
            CreateTransferRequest(
                idempotency_key=body.idempotencyKey,
                from_wallet_id=body.fromWalletId,
                to_wallet_id=body.toWalletId,
                amount=body.amount,
            )
        )
    except InvalidTransferError as error:
        raise HTTPException(status_code=422, detail=str(error)) from error
    except WalletNotFoundError as error:
        raise HTTPException(status_code=404, detail=str(error)) from error
    except IdempotencyKeyReusedError as error:
        raise HTTPException(status_code=409, detail=str(error)) from error
    except RetryExhaustedError as error:
        raise HTTPException(status_code=503, detail=str(error)) from error

    return _to_response(transfer, idempotency_key=body.idempotencyKey)


def _to_response(transfer: Transfer, *, idempotency_key: str) -> TransferResponse:
    return TransferResponse(
        id=transfer.id,
        idempotencyKey=idempotency_key,
        fromWalletId=transfer.from_wallet_id,
        toWalletId=transfer.to_wallet_id,
        amount=transfer.amount,
        status=Status(transfer.status.value),
        failureReason=transfer.failure_reason,
        createdAt=transfer.created_at,
        updatedAt=transfer.updated_at,
    )
