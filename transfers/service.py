from django.db import transaction as db_transaction
from django.db.utils import IntegrityError

from .models import Wallet
from .repository import WalletRepository, TransactionRepository, IdempotencyRepository, LedgerRepository

wallet_repo = WalletRepository()
txn_repo = TransactionRepository()
idempotency_repo = IdempotencyRepository()
ledger_repo = LedgerRepository()


def create_transfer(idempotency_key, from_wallet_id, to_wallet_id, amount):
    # Fast-path: return existing result for duplicate requests
    record = idempotency_repo.get(idempotency_key)
    if record:
        return record.transaction_id, False

    if amount <= 0:
        raise ValueError("Amount must be positive")
    if from_wallet_id == to_wallet_id:
        raise ValueError("Cannot transfer to the same wallet")

    try:
        with db_transaction.atomic():
            # Lock wallets in sorted id order to prevent deadlocks under concurrent requests
            ids = sorted([from_wallet_id, to_wallet_id])
            wallets = {
                w.id: w
                for w in Wallet.objects.select_for_update().filter(id__in=ids)
            }

            from_wallet = wallets.get(from_wallet_id)
            to_wallet = wallets.get(to_wallet_id)

            if from_wallet is None:
                raise ValueError(f"Wallet {from_wallet_id} not found")
            if to_wallet is None:
                raise ValueError(f"Wallet {to_wallet_id} not found")

            txn = txn_repo.create(from_wallet, to_wallet, amount, 'PENDING')
            idempotency_repo.create(idempotency_key, txn)

            if from_wallet.balance < amount:
                txn_repo.update_status(txn, 'FAILED')
                return txn, True

            ledger_repo.create_entries(from_wallet, to_wallet, txn)

            from_wallet.balance -= amount
            to_wallet.balance += amount
            wallet_repo.save(from_wallet)
            wallet_repo.save(to_wallet)

            txn_repo.update_status(txn, 'PROCESSED')
            return txn, True

    except IntegrityError:
        # Concurrent request with the same idempotency key won the race
        record = idempotency_repo.get(idempotency_key)
        if record:
            return record.transaction_id, False
        raise
