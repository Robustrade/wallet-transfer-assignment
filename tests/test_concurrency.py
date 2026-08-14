import threading

from app.db.base import SessionLocal
from app.domain.enums import TransferStatus
from app.services import transfer_service


def test_concurrent_debits_never_overdraw(seed_wallet, balance_of):
    # Source funded for exactly one transfer of 100; ten requests race for it.
    seed_wallet("w_src", 100)
    seed_wallet("w_dst", 0)

    outcomes: list[TransferStatus] = []
    lock = threading.Lock()

    def worker(i: int):
        session = SessionLocal()
        try:
            outcome = transfer_service.create_transfer(
                session,
                from_wallet_id="w_src",
                to_wallet_id="w_dst",
                amount=100,
                idempotency_key=f"race-{i}",
            )
            with lock:
                outcomes.append(outcome.status)
        finally:
            session.close()

    threads = [threading.Thread(target=worker, args=(i,)) for i in range(10)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    processed = [s for s in outcomes if s == TransferStatus.PROCESSED]
    assert len(processed) == 1
    assert balance_of("w_src") == 0
    assert balance_of("w_dst") == 100
