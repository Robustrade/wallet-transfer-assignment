from fastapi import APIRouter, Depends
from fastapi.responses import JSONResponse
from sqlalchemy.orm import Session

from app.api.schemas import TransferRequest
from app.db.base import get_session
from app.domain.enums import TransferStatus
from app.domain.errors import WalletNotFound
from app.repositories import transfer, wallet
from app.services import transfer_service

router = APIRouter()


@router.post("/transfers")
def create_transfer(payload: TransferRequest, session: Session = Depends(get_session)):
    outcome = transfer_service.create_transfer(
        session,
        from_wallet_id=payload.from_wallet_id,
        to_wallet_id=payload.to_wallet_id,
        amount=payload.amount,
        idempotency_key=payload.idempotency_key,
    )

    body = {
        "transferId": outcome.transfer_id,
        "status": outcome.status.value,
        "fromWalletId": outcome.from_wallet_id,
        "toWalletId": outcome.to_wallet_id,
        "amount": outcome.amount,
    }

    if outcome.status == TransferStatus.FAILED:
        body["code"] = outcome.failure_code
        return JSONResponse(status_code=422, content=body)

    return JSONResponse(status_code=200 if outcome.replayed else 201, content=body)


@router.get("/wallets/{wallet_id}")
def get_wallet(wallet_id: str, session: Session = Depends(get_session)):
    row = wallet.get(session, wallet_id)
    if row is None:
        raise WalletNotFound()
    return {"walletId": row.id, "balance": row.balance}


@router.get("/transfers/{transfer_id}")
def get_transfer(transfer_id: str, session: Session = Depends(get_session)):
    row = transfer.get(session, transfer_id)
    if row is None:
        return JSONResponse(status_code=404, content={"code": "TRANSFER_NOT_FOUND"})
    return {
        "transferId": row.id,
        "status": row.status,
        "fromWalletId": row.from_wallet,
        "toWalletId": row.to_wallet,
        "amount": row.amount,
    }
