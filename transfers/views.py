import json

from django.db.utils import OperationalError
from django.http import JsonResponse
from django.views import View
from django.utils.decorators import method_decorator
from django.views.decorators.csrf import csrf_exempt

from .service import create_transfer


@method_decorator(csrf_exempt, name='dispatch')
class TransferView(View):
    def post(self, request):
        try:
            data = json.loads(request.body)
        except (json.JSONDecodeError, ValueError):
            return JsonResponse({'error': 'Invalid JSON'}, status=400)

        idempotency_key = data.get('idempotencyKey')
        from_wallet_id = data.get('fromWalletId')
        to_wallet_id = data.get('toWalletId')
        amount = data.get('amount')

        if not all([idempotency_key, from_wallet_id is not None, to_wallet_id is not None, amount is not None]):
            return JsonResponse({'error': 'Missing required fields: idempotencyKey, fromWalletId, toWalletId, amount'}, status=400)

        try:
            txn, is_new = create_transfer(idempotency_key, from_wallet_id, to_wallet_id, amount)
        except ValueError as e:
            return JsonResponse({'error': str(e)}, status=400)
        except OperationalError:
            return JsonResponse({'error': 'Service temporarily unavailable, please retry'}, status=503)

        body = {
            'transactionId': txn.id,
            'status': txn.status,
            'fromWalletId': txn.from_wallet_id_id,
            'toWalletId': txn.to_wallet_id_id,
            'amount': txn.amount,
        }

        if txn.status == 'FAILED':
            return JsonResponse(body, status=422)
        if is_new:
            return JsonResponse(body, status=201)
        return JsonResponse(body, status=200)
