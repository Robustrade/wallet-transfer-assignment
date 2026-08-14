def _payload(**overrides):
    payload = {
        "idempotencyKey": "idem-1",
        "fromWalletId": "w_src",
        "toWalletId": "w_dst",
        "amount": 100,
    }
    payload.update(overrides)
    return payload


def test_same_key_and_body_transfers_once(client, seed_wallet, balance_of):
    seed_wallet("w_src", 500)
    seed_wallet("w_dst", 0)

    first = client.post("/transfers", json=_payload())
    second = client.post("/transfers", json=_payload())

    assert first.status_code == 201
    assert second.status_code == 200  # replay
    assert first.json()["transferId"] == second.json()["transferId"]
    # money moved exactly once
    assert balance_of("w_src") == 400
    assert balance_of("w_dst") == 100


def test_same_key_different_body_conflicts(client, seed_wallet):
    seed_wallet("w_src", 500)
    seed_wallet("w_dst", 0)

    client.post("/transfers", json=_payload(amount=100))
    conflict = client.post("/transfers", json=_payload(amount=200))

    assert conflict.status_code == 409
    assert conflict.json()["code"] == "IDEMPOTENCY_KEY_CONFLICT"


def test_failed_transfer_is_also_idempotent(client, seed_wallet, balance_of):
    seed_wallet("w_src", 50)
    seed_wallet("w_dst", 0)

    first = client.post("/transfers", json=_payload(amount=100))
    second = client.post("/transfers", json=_payload(amount=100))

    assert first.status_code == 422
    assert second.status_code == 422
    assert first.json()["transferId"] == second.json()["transferId"]
    assert balance_of("w_src") == 50
