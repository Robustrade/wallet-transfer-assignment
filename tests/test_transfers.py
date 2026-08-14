def _payload(**overrides):
    payload = {
        "idempotencyKey": "key-1",
        "fromWalletId": "w_src",
        "toWalletId": "w_dst",
        "amount": 100,
    }
    payload.update(overrides)
    return payload


def test_transfer_moves_funds_and_records_ledger(client, seed_wallet, balance_of):
    seed_wallet("w_src", 500)
    seed_wallet("w_dst", 0)

    resp = client.post("/transfers", json=_payload())

    assert resp.status_code == 201
    body = resp.json()
    assert body["status"] == "PROCESSED"
    assert balance_of("w_src") == 400
    assert balance_of("w_dst") == 100

    ledger = client.get(f"/transfers/{body['transferId']}")
    assert ledger.status_code == 200


def test_insufficient_funds_fails_without_moving_money(client, seed_wallet, balance_of):
    seed_wallet("w_src", 50)
    seed_wallet("w_dst", 0)

    resp = client.post("/transfers", json=_payload(amount=100))

    assert resp.status_code == 422
    assert resp.json()["code"] == "INSUFFICIENT_FUNDS"
    assert balance_of("w_src") == 50
    assert balance_of("w_dst") == 0


def test_unknown_wallet_returns_404(client, seed_wallet):
    seed_wallet("w_src", 100)
    resp = client.post("/transfers", json=_payload(toWalletId="missing"))
    assert resp.status_code == 404
    assert resp.json()["code"] == "WALLET_NOT_FOUND"


def test_non_positive_amount_rejected(client, seed_wallet):
    seed_wallet("w_src", 100)
    seed_wallet("w_dst", 0)
    resp = client.post("/transfers", json=_payload(amount=0))
    assert resp.status_code == 400
    assert resp.json()["code"] == "INVALID_REQUEST"


def test_self_transfer_rejected(client, seed_wallet):
    seed_wallet("w_src", 100)
    resp = client.post("/transfers", json=_payload(toWalletId="w_src"))
    assert resp.status_code == 400
