import json
import threading

from django.test import TestCase, TransactionTestCase, Client

from .models import Wallet, Transaction, LedgerEntries, IdempotencyRecord


def post_transfer(client, idempotency_key, from_wallet_id, to_wallet_id, amount):
    return client.post(
        '/transfers',
        data=json.dumps({
            'idempotencyKey': idempotency_key,
            'fromWalletId': from_wallet_id,
            'toWalletId': to_wallet_id,
            'amount': amount,
        }),
        content_type='application/json',
    )


class TransferEndpointTests(TestCase):
    def setUp(self):
        self.client = Client()
        self.wallet_a = Wallet.objects.create(balance=500)
        self.wallet_b = Wallet.objects.create(balance=100)

    def test_successful_transfer(self):
        resp = post_transfer(self.client, 'key1', self.wallet_a.id, self.wallet_b.id, 200)

        self.assertEqual(resp.status_code, 201)
        body = resp.json()
        self.assertEqual(body['status'], 'PROCESSED')
        self.assertEqual(body['amount'], 200)

        self.wallet_a.refresh_from_db()
        self.wallet_b.refresh_from_db()
        self.assertEqual(self.wallet_a.balance, 300)
        self.assertEqual(self.wallet_b.balance, 300)

    def test_idempotent_replay_returns_same_transaction(self):
        resp1 = post_transfer(self.client, 'key2', self.wallet_a.id, self.wallet_b.id, 50)
        resp2 = post_transfer(self.client, 'key2', self.wallet_a.id, self.wallet_b.id, 50)

        self.assertEqual(resp1.json()['transactionId'], resp2.json()['transactionId'])
        self.assertEqual(Transaction.objects.count(), 1)
        self.assertEqual(IdempotencyRecord.objects.count(), 1)

        self.wallet_a.refresh_from_db()
        self.assertEqual(self.wallet_a.balance, 450)  # deducted only once

    def test_idempotent_replay_returns_200(self):
        post_transfer(self.client, 'key3', self.wallet_a.id, self.wallet_b.id, 50)
        resp2 = post_transfer(self.client, 'key3', self.wallet_a.id, self.wallet_b.id, 50)

        self.assertEqual(resp2.status_code, 200)

    def test_insufficient_balance_returns_422_and_failed_status(self):
        resp = post_transfer(self.client, 'key4', self.wallet_b.id, self.wallet_a.id, 200)

        self.assertEqual(resp.status_code, 422)
        self.assertEqual(resp.json()['status'], 'FAILED')

        self.wallet_b.refresh_from_db()
        self.wallet_a.refresh_from_db()
        self.assertEqual(self.wallet_b.balance, 100)  # unchanged
        self.assertEqual(self.wallet_a.balance, 500)  # unchanged

    def test_insufficient_balance_creates_no_ledger_entries(self):
        post_transfer(self.client, 'key5', self.wallet_b.id, self.wallet_a.id, 200)

        self.assertEqual(LedgerEntries.objects.count(), 0)

    def test_ledger_entries_created_correctly(self):
        post_transfer(self.client, 'key6', self.wallet_a.id, self.wallet_b.id, 100)

        entries = LedgerEntries.objects.all()
        self.assertEqual(entries.count(), 2)

        debit = entries.get(transaction_type='DEBIT')
        credit = entries.get(transaction_type='CREDIT')
        self.assertEqual(debit.wallet_id_id, self.wallet_a.id)
        self.assertEqual(credit.wallet_id_id, self.wallet_b.id)

    def test_invalid_amount_zero(self):
        resp = post_transfer(self.client, 'key7', self.wallet_a.id, self.wallet_b.id, 0)
        self.assertEqual(resp.status_code, 400)

    def test_invalid_amount_negative(self):
        resp = post_transfer(self.client, 'key8', self.wallet_a.id, self.wallet_b.id, -50)
        self.assertEqual(resp.status_code, 400)

    def test_same_wallet_transfer(self):
        resp = post_transfer(self.client, 'key9', self.wallet_a.id, self.wallet_a.id, 50)
        self.assertEqual(resp.status_code, 400)

    def test_wallet_not_found(self):
        resp = post_transfer(self.client, 'key10', 9999, self.wallet_b.id, 50)
        self.assertEqual(resp.status_code, 400)

    def test_missing_fields_returns_400(self):
        resp = self.client.post(
            '/transfers',
            data=json.dumps({'idempotencyKey': 'key11'}),
            content_type='application/json',
        )
        self.assertEqual(resp.status_code, 400)

    def test_idempotent_failed_transfer_replays_as_failed(self):
        # A failed transfer replayed with the same key returns the same FAILED result
        resp1 = post_transfer(self.client, 'key12', self.wallet_b.id, self.wallet_a.id, 200)
        resp2 = post_transfer(self.client, 'key12', self.wallet_b.id, self.wallet_a.id, 200)

        self.assertEqual(resp1.json()['transactionId'], resp2.json()['transactionId'])
        self.assertEqual(resp2.json()['status'], 'FAILED')
        self.assertEqual(Transaction.objects.count(), 1)


class ConcurrencyTests(TransactionTestCase):
    def test_concurrent_transfers_no_double_spend(self):
        wallet_a = Wallet.objects.create(balance=100)
        wallet_b = Wallet.objects.create(balance=0)

        statuses = []

        def attempt_transfer(key):
            client = Client()
            resp = post_transfer(client, key, wallet_a.id, wallet_b.id, 100)
            statuses.append(resp.status_code)

        # Two concurrent threads each try to debit the full balance
        t1 = threading.Thread(target=attempt_transfer, args=('c-key1',))
        t2 = threading.Thread(target=attempt_transfer, args=('c-key2',))
        t1.start()
        t2.start()
        t1.join()
        t2.join()

        wallet_a.refresh_from_db()
        # Balance must never go negative regardless of which thread won
        self.assertGreaterEqual(wallet_a.balance, 0)

        # Each response must be: 201 (processed), 422 (failed - insufficient funds),
        # or 503 (SQLite lock contention — expected under concurrent writes)
        for code in statuses:
            self.assertIn(code, [201, 422, 503])

        # At most one transfer can fully succeed
        self.assertLessEqual(statuses.count(201), 1)
