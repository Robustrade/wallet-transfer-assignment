from pydantic import BaseModel, ConfigDict, Field, model_validator


class TransferRequest(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    idempotency_key: str = Field(alias="idempotencyKey", min_length=1)
    from_wallet_id: str = Field(alias="fromWalletId", min_length=1)
    to_wallet_id: str = Field(alias="toWalletId", min_length=1)
    amount: int = Field(gt=0)

    @model_validator(mode="after")
    def _distinct_wallets(self):
        if self.from_wallet_id == self.to_wallet_id:
            raise ValueError("fromWalletId and toWalletId must differ")
        return self


class TransferResponse(BaseModel):
    transferId: str
    status: str
    fromWalletId: str
    toWalletId: str
    amount: int


class WalletResponse(BaseModel):
    walletId: str
    balance: int
