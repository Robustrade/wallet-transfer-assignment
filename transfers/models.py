from django.db import models

# Create your models here.

"""
Need a transaction table:
    id: int
    from_wallet_id: id
    to_wallet_id: id
    amount: int
    status: PENDING | PROCESSED | FAILED

wallet table
    id: int
    balance: int
(update during transactions)

ledger_entries table
entry_id: auto inc int
wallet_id: id
transfer_id: id
type: DEBIT | CREDIT
amount: int

idempotency_records:
    key: unique string
    transaction_id: id

(transactions with row level locking, probs)

"""

class Wallet(models.Model):
    balance = models.IntegerField(null=False)

class Transaction(models.Model):
    class Status(models.TextChoices):
        PENDING = "PENDING"
        PROCESSED = "PROCESSED"
        FAILED = "FAILED"

    from_wallet_id = models.ForeignKey(Wallet, on_delete=models.RESTRICT, related_name="sender_wallet")
    to_wallet_id = models.ForeignKey(Wallet, on_delete=models.RESTRICT, related_name="receiver_wallet")
    amount = models.IntegerField(null=False)
    status = models.CharField(max_length=9, choices=Status.choices)

class IdempotencyRecord(models.Model):
    key = models.CharField(max_length=16, unique=True)
    transaction_id = models.ForeignKey(Transaction, on_delete=models.RESTRICT)

class LedgerEntries(models.Model):
    class TransactionType(models.TextChoices):
        DEBIT = "DEBIT"
        CREDIT = "CREDIT"

    wallet_id = models.ForeignKey(Wallet, on_delete=models.RESTRICT)
    transaction_id = models.ForeignKey(Transaction, on_delete=models.RESTRICT)
    transaction_type = models.CharField(max_length=6, choices=TransactionType.choices)
    amount = models.IntegerField(null=False)
