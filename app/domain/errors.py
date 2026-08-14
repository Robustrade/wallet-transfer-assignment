class DomainError(Exception):
    """Base for errors that map to a defined API response."""

    code = "INTERNAL_ERROR"
    http_status = 500

    def __init__(self, message: str | None = None):
        self.message = message or self.code
        super().__init__(self.message)


class WalletNotFound(DomainError):
    code = "WALLET_NOT_FOUND"
    http_status = 404


class IdempotencyConflict(DomainError):
    """Same idempotency key reused with a different request body."""

    code = "IDEMPOTENCY_KEY_CONFLICT"
    http_status = 409


class TransferInProgress(DomainError):
    """An earlier request with this key is still being processed."""

    code = "TRANSFER_IN_PROGRESS"
    http_status = 409
