from .models import Wallet, Transaction, IdempotencyRecord, LedgerEntries


class WalletRepository:
    def get_for_update(self, wallet_id):
        return Wallet.objects.select_for_update().get(id=wallet_id)

    def save(self, wallet):
        wallet.save()


class TransactionRepository:
    def create(self, from_wallet, to_wallet, amount, status):
        return Transaction.objects.create(
            from_wallet_id=from_wallet,
            to_wallet_id=to_wallet,
            amount=amount,
            status=status,
        )

    def update_status(self, txn, status):
        txn.status = status
        txn.save(update_fields=['status'])


class IdempotencyRepository:
    def get(self, key):
        try:
            return IdempotencyRecord.objects.select_related('transaction_id').get(key=key)
        except IdempotencyRecord.DoesNotExist:
            return None

    def create(self, key, transaction):
        return IdempotencyRecord.objects.create(key=key, transaction_id=transaction)


class LedgerRepository:
    def create_entries(self, from_wallet, to_wallet, transaction):
        LedgerEntries.objects.create(
            wallet_id=from_wallet,
            transaction_id=transaction,
            transaction_type='DEBIT',
            amount=transaction.amount,
        )
        LedgerEntries.objects.create(
            wallet_id=to_wallet,
            transaction_id=transaction,
            transaction_type='CREDIT',
            amount=transaction.amount,
        )
