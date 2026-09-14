#!/usr/bin/env python3
"""
API-level integration suite against a running wallet-transfer service.

Mirrors the Java integration tests in src/test/java/.../integration/
using HTTP calls only (statement API used to assert balances).

Usage:
  python3 scripts/api_integration_tests.py [--base-url http://localhost:8080]
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import sys
import time
import traceback
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path
from typing import Any, Callable, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


@dataclass
class TestResult:
    name: str
    api: str
    summary: str
    status: str  # PASS | FAIL | SKIP
    detail: str = ""
    http_status: Optional[int] = None
    duration_ms: int = 0


@dataclass
class Report:
    base_url: str
    started_at: str
    finished_at: str = ""
    results: list[TestResult] = field(default_factory=list)

    @property
    def passed(self) -> int:
        return sum(1 for r in self.results if r.status == "PASS")

    @property
    def failed(self) -> int:
        return sum(1 for r in self.results if r.status == "FAIL")

    @property
    def skipped(self) -> int:
        return sum(1 for r in self.results if r.status == "SKIP")


class ApiClient:
    def __init__(self, base_url: str, timeout: float = 30.0) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        body: Optional[dict[str, Any]] = None,
        query: Optional[dict[str, Any]] = None,
    ) -> tuple[int, Any]:
        url = f"{self.base_url}{path}"
        if query:
            filtered = {k: v for k, v in query.items() if v is not None}
            if filtered:
                url = f"{url}?{urlencode(filtered)}"
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = Request(url, data=data, headers=headers, method=method)
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
                payload = json.loads(raw) if raw else None
                return resp.status, payload
        except HTTPError as exc:
            raw = exc.read().decode("utf-8")
            try:
                payload = json.loads(raw) if raw else None
            except json.JSONDecodeError:
                payload = {"raw": raw}
            return exc.code, payload
        except URLError as exc:
            raise RuntimeError(f"Request failed: {method} {url}: {exc}") from exc


def dec(value: Any) -> Decimal:
    return Decimal(str(value))


def assert_true(cond: bool, message: str) -> None:
    if not cond:
        raise AssertionError(message)


class Suite:
    def __init__(self, client: ApiClient) -> None:
        self.client = client
        self._user_seq = int(time.time() * 1000) % 1_000_000_000

    def next_user_id(self) -> int:
        self._user_seq += 1
        return self._user_seq

    def create_wallet(self, user_id: int, balance: str) -> tuple[int, dict]:
        return self.client.request(
            "POST",
            "/wallets",
            {"userId": user_id, "balance": balance},
        )

    def create_transfer(
        self,
        key: str,
        from_wallet: str,
        to_wallet: str,
        amount: str,
    ) -> tuple[int, dict]:
        return self.client.request(
            "POST",
            "/transfers",
            {
                "idempotencyKey": key,
                "fromWalletId": from_wallet,
                "toWalletId": to_wallet,
                "amount": amount,
            },
        )

    def get_statement(
        self,
        user_id: Optional[int] = None,
        wallet_id: Optional[str] = None,
    ) -> tuple[int, dict]:
        query: dict[str, Any] = {}
        if user_id is not None:
            query["userId"] = user_id
        if wallet_id is not None:
            query["walletId"] = wallet_id
        return self.client.request("GET", "/statements", query=query)

    def balance(self, wallet_id: str) -> Decimal:
        status, body = self.get_statement(wallet_id=wallet_id)
        assert_true(status == 200, f"statement for balance check failed: {status} {body}")
        return dec(body["currentBalance"])

    # --- Wallet ---

    def test_create_wallet(self) -> None:
        user_id = self.next_user_id()
        status, body = self.create_wallet(user_id, "1000.00")
        assert_true(status == 201, f"expected 201, got {status}: {body}")
        assert_true(body.get("walletId"), "walletId missing")
        assert_true(body.get("userId") == user_id, "userId mismatch")
        assert_true(dec(body["balance"]) == Decimal("1000.00"), "balance mismatch")
        assert_true(body.get("createdAt"), "createdAt missing")
        assert_true(self.balance(body["walletId"]) == Decimal("1000.00"), "persisted balance mismatch")

    def test_duplicate_wallet(self) -> None:
        user_id = self.next_user_id()
        status, _ = self.create_wallet(user_id, "100.00")
        assert_true(status == 201, f"setup create failed: {status}")
        status, body = self.create_wallet(user_id, "50.00")
        assert_true(status == 409, f"expected 409, got {status}: {body}")
        assert_true(body.get("errorCode") == "WALLET_ALREADY_EXISTS", f"errorCode={body}")

    def test_negative_balance(self) -> None:
        status, body = self.create_wallet(self.next_user_id(), "-1.00")
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "VALIDATION_ERROR", f"errorCode={body}")

    # --- Transfer ---

    def test_transfer_success(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "500.00")
        _, to_w = self.create_wallet(self.next_user_id(), "50.00")
        status, body = self.create_transfer(
            f"xfer-success-{uuid.uuid4()}",
            from_w["walletId"],
            to_w["walletId"],
            "100.00",
        )
        assert_true(status == 201, f"expected 201, got {status}: {body}")
        assert_true(body.get("state") == "PROCESSED", f"state={body}")
        assert_true(body.get("transferId"), "transferId missing")
        assert_true(body.get("failureReason") in (None, ""), f"failureReason={body}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("400.00"), "from balance")
        assert_true(self.balance(to_w["walletId"]) == Decimal("150.00"), "to balance")
        # Ledger presence inferred via statement entries (funding + transfer)
        _, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        _, to_stmt = self.get_statement(wallet_id=to_w["walletId"])
        assert_true(len(from_stmt["entries"]) == 2, "from funding + debit")
        assert_true(len(to_stmt["entries"]) == 2, "to funding + credit")

    def test_wallet_missing(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "100.00")
        status, body = self.create_transfer(
            f"xfer-missing-{uuid.uuid4()}",
            from_w["walletId"],
            str(uuid.uuid4()),
            "10.00",
        )
        assert_true(status == 404, f"expected 404, got {status}: {body}")
        assert_true(body.get("errorCode") == "WALLETS_NOT_FOUND", f"errorCode={body}")

    def test_insufficient_balance(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "40.00")
        _, to_w = self.create_wallet(self.next_user_id(), "10.00")
        status, body = self.create_transfer(
            f"xfer-insufficient-{uuid.uuid4()}",
            from_w["walletId"],
            to_w["walletId"],
            "100.00",
        )
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "INSUFFICIENT_BALANCE", f"errorCode={body}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("40.00"), "from unchanged")
        assert_true(self.balance(to_w["walletId"]) == Decimal("10.00"), "to unchanged")
        _, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        assert_true(len(from_stmt["entries"]) == 1, "only funding ledger on insufficient")
        assert_true(from_stmt["entries"][0].get("transferId") is None, "funding only")

    def test_idempotent_replay(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "500.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        key = f"xfer-idempotent-{uuid.uuid4()}"
        status1, first = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "75.00")
        status2, second = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "75.00")
        assert_true(status1 == 201 and status2 == 201, f"statuses {status1}/{status2}")
        assert_true(first["transferId"] == second["transferId"], "transferId should match")
        assert_true(second.get("state") == "PROCESSED", f"state={second}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("425.00"), "no double debit")
        assert_true(self.balance(to_w["walletId"]) == Decimal("75.00"), "to balance")

    def test_idempotency_conflict(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "500.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        key = f"xfer-conflict-{uuid.uuid4()}"
        status1, _ = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "50.00")
        assert_true(status1 == 201, f"first transfer failed: {status1}")
        status2, body = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "60.00")
        assert_true(status2 == 409, f"expected 409, got {status2}: {body}")
        assert_true(body.get("errorCode") == "IDEMPOTENCY_CONFLICT", f"errorCode={body}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("450.00"), "from after first only")
        assert_true(self.balance(to_w["walletId"]) == Decimal("50.00"), "to after first only")

    def test_failed_then_retry_after_topup(self) -> None:
        """Top-up via funding wallet transfer (API-only substitute for SQL UPDATE)."""
        _, from_w = self.create_wallet(self.next_user_id(), "40.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        _, funder = self.create_wallet(self.next_user_id(), "1000.00")
        key = f"xfer-retry-{uuid.uuid4()}"
        status1, body1 = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(status1 == 400, f"expected first fail 400, got {status1}: {body1}")
        assert_true(body1.get("errorCode") == "INSUFFICIENT_BALANCE", f"errorCode={body1}")

        fund_status, _ = self.create_transfer(
            f"fund-{uuid.uuid4()}",
            funder["walletId"],
            from_w["walletId"],
            "250.00",
        )
        assert_true(fund_status == 201, f"top-up failed: {fund_status}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("290.00"), "after top-up")

        status2, retried = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(status2 == 201, f"retry expected 201, got {status2}: {retried}")
        assert_true(retried.get("state") == "PROCESSED", f"state={retried}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("190.00"), "from after retry")
        assert_true(self.balance(to_w["walletId"]) == Decimal("100.00"), "to after retry")

    def test_invalid_amount(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "100.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        status, body = self.create_transfer(
            f"xfer-invalid-amt-{uuid.uuid4()}",
            from_w["walletId"],
            to_w["walletId"],
            "0.00",
        )
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "VALIDATION_ERROR", f"errorCode={body}")

    def test_same_wallet(self) -> None:
        _, wallet = self.create_wallet(self.next_user_id(), "100.00")
        status, body = self.create_transfer(
            f"xfer-same-{uuid.uuid4()}",
            wallet["walletId"],
            wallet["walletId"],
            "10.00",
        )
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "VALIDATION_ERROR", f"errorCode={body}")

    # --- Statement ---

    def test_statement_after_transfer(self) -> None:
        from_user = self.next_user_id()
        to_user = self.next_user_id()
        _, from_w = self.create_wallet(from_user, "1000.00")
        _, to_w = self.create_wallet(to_user, "0.00")
        status, _ = self.create_transfer(
            f"stmt-xfer-{uuid.uuid4()}",
            from_w["walletId"],
            to_w["walletId"],
            "100.00",
        )
        assert_true(status == 201, f"transfer failed: {status}")

        st_from, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        assert_true(st_from == 200, f"from statement {st_from}")
        assert_true(from_stmt["userId"] == from_user, "from userId")
        assert_true(dec(from_stmt["currentBalance"]) == Decimal("900.00"), "from balance")
        assert_true(len(from_stmt["entries"]) == 2, "from entries (funding + debit)")
        funding = from_stmt["entries"][0]
        assert_true(funding["transferType"] == "CREDIT", f"funding type={funding}")
        assert_true(funding.get("transferId") is None, "funding has no transferId")
        assert_true(dec(funding["amount"]) == Decimal("1000.00"), "funding amount")
        entry = from_stmt["entries"][1]
        assert_true(entry["transferType"] == "DEBIT", f"type={entry}")
        assert_true(dec(entry["amount"]) == Decimal("100.00"), "amount")
        assert_true(dec(entry["previousBalance"]) == Decimal("1000.00"), "prev")
        assert_true(dec(entry["balanceAfterTransfer"]) == Decimal("900.00"), "after")

        st_to, to_stmt = self.get_statement(wallet_id=to_w["walletId"])
        assert_true(st_to == 200, f"to statement {st_to}")
        assert_true(dec(to_stmt["currentBalance"]) == Decimal("100.00"), "to balance")
        # Zero opening balance → no funding row; only transfer credit.
        assert_true(len(to_stmt["entries"]) == 1, "to entries")
        assert_true(to_stmt["entries"][0]["transferType"] == "CREDIT", "credit")
        assert_true(dec(to_stmt["entries"][0]["previousBalance"]) == Decimal("0.00"), "to prev")
        assert_true(dec(to_stmt["entries"][0]["balanceAfterTransfer"]) == Decimal("100.00"), "to after")

    def test_statement_by_user(self) -> None:
        user_id = self.next_user_id()
        _, wallet = self.create_wallet(user_id, "250.00")
        status, body = self.get_statement(user_id=user_id)
        assert_true(status == 200, f"expected 200, got {status}: {body}")
        assert_true(body["walletId"] == wallet["walletId"], "walletId")
        assert_true(dec(body["currentBalance"]) == Decimal("250.00"), "balance")
        assert_true(len(body.get("entries") or []) == 1, "funding entry present")
        funding = body["entries"][0]
        assert_true(funding.get("transferId") is None, "funding has no transferId")
        assert_true(funding["transferType"] == "CREDIT", "funding credit")
        assert_true(dec(funding["amount"]) == Decimal("250.00"), "funding amount")

    def test_statement_missing_params(self) -> None:
        status, body = self.get_statement()
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "VALIDATION_ERROR", f"errorCode={body}")

    def test_statement_wallet_missing(self) -> None:
        status, body = self.get_statement(wallet_id="missing-wallet")
        assert_true(status == 404, f"expected 404, got {status}: {body}")
        assert_true(body.get("errorCode") == "WALLET_NOT_FOUND", f"errorCode={body}")

    def test_one_wallet_per_user_enforced(self) -> None:
        """Second wallet for same userId is rejected by API (and by unique index)."""
        user_id = self.next_user_id()
        status, first = self.create_wallet(user_id, "10.00")
        assert_true(status == 201, f"setup wallet failed: {status}")
        status2, body2 = self.create_wallet(user_id, "20.00")
        assert_true(status2 == 409, f"expected 409, got {status2}: {body2}")
        assert_true(body2.get("errorCode") == "WALLET_ALREADY_EXISTS", f"errorCode={body2}")
        st, stmt = self.get_statement(user_id=user_id)
        assert_true(st == 200, f"expected 200, got {st}: {stmt}")
        assert_true(stmt.get("walletId") == first["walletId"], "statement wallet matches")

    def test_statement_user_mismatch(self) -> None:
        _, wallet_a = self.create_wallet(self.next_user_id(), "10.00")
        user_b = self.next_user_id()
        self.create_wallet(user_b, "10.00")
        status, body = self.get_statement(user_id=user_b, wallet_id=wallet_a["walletId"])
        assert_true(status == 400, f"expected 400, got {status}: {body}")
        assert_true(body.get("errorCode") == "WALLET_USER_MISMATCH", f"errorCode={body}")

    def test_statement_both_params(self) -> None:
        user_id = self.next_user_id()
        _, wallet = self.create_wallet(user_id, "33.00")
        status, body = self.get_statement(user_id=user_id, wallet_id=wallet["walletId"])
        assert_true(status == 200, f"expected 200, got {status}: {body}")
        assert_true(body["walletId"] == wallet["walletId"], "walletId")
        assert_true(body["userId"] == user_id, "userId")
        assert_true(dec(body["currentBalance"]) == Decimal("33.00"), "balance")

    # --- Concurrent / E2E ---

    def test_concurrent_no_overdraw(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "100.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        success = 0
        insufficient = 0

        def one(i: int) -> tuple[int, str]:
            status, body = self.create_transfer(
                f"concurrent-{uuid.uuid4()}-{i}",
                from_w["walletId"],
                to_w["walletId"],
                "10.00",
            )
            code = (body or {}).get("errorCode", "")
            return status, code

        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            futures = [pool.submit(one, i) for i in range(20)]
            for fut in concurrent.futures.as_completed(futures):
                status, code = fut.result()
                if status == 201:
                    success += 1
                elif status == 400 and code == "INSUFFICIENT_BALANCE":
                    insufficient += 1
                else:
                    raise AssertionError(f"unexpected result status={status} code={code}")

        assert_true(success == 10, f"success={success}")
        assert_true(insufficient == 10, f"insufficient={insufficient}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("0.00"), "from final")
        assert_true(self.balance(to_w["walletId"]) == Decimal("100.00"), "to final")
        _, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        _, to_stmt = self.get_statement(wallet_id=to_w["walletId"])
        assert_true(len(from_stmt["entries"]) == 11, "funding + 10 debit entries")
        assert_true(len(to_stmt["entries"]) == 10, "10 credit entries (zero opening)")

    def test_end_to_end(self) -> None:
        alice_user = self.next_user_id()
        bob_user = self.next_user_id()
        _, alice = self.create_wallet(alice_user, "1000.00")
        _, bob = self.create_wallet(bob_user, "100.00")
        s1, first = self.create_transfer(
            f"e2e-1-{uuid.uuid4()}", alice["walletId"], bob["walletId"], "250.00"
        )
        s2, second = self.create_transfer(
            f"e2e-2-{uuid.uuid4()}", bob["walletId"], alice["walletId"], "50.00"
        )
        assert_true(s1 == 201 and first.get("state") == "PROCESSED", f"first={s1}/{first}")
        assert_true(s2 == 201 and second.get("state") == "PROCESSED", f"second={s2}/{second}")
        assert_true(self.balance(alice["walletId"]) == Decimal("800.00"), "alice")
        assert_true(self.balance(bob["walletId"]) == Decimal("300.00"), "bob")

        _, alice_stmt = self.get_statement(wallet_id=alice["walletId"])
        assert_true(len(alice_stmt["entries"]) == 3, "alice entries")
        assert_true(dec(alice_stmt["currentBalance"]) == Decimal("800.00"), "alice bal")
        assert_true(
            dec(alice_stmt["entries"][0]["balanceAfterTransfer"]) == Decimal("1000.00"),
            "alice funding",
        )
        assert_true(
            dec(alice_stmt["entries"][1]["balanceAfterTransfer"]) == Decimal("750.00"),
            "alice after first",
        )
        assert_true(
            dec(alice_stmt["entries"][2]["balanceAfterTransfer"]) == Decimal("800.00"),
            "alice after second",
        )
        _, bob_stmt = self.get_statement(user_id=bob_user)
        assert_true(bob_stmt["walletId"] == bob["walletId"], "bob wallet")
        assert_true(dec(bob_stmt["currentBalance"]) == Decimal("300.00"), "bob bal")
        assert_true(len(bob_stmt["entries"]) == 3, "bob entries")

    # --- Concurrency handling ---

    def test_concurrent_same_idempotency_key(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "200.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        key = f"concurrent-same-key-{uuid.uuid4()}"
        bodies: list[dict] = []

        def one(_: int) -> tuple[int, dict]:
            return self.create_transfer(key, from_w["walletId"], to_w["walletId"], "50.00")

        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            futures = [pool.submit(one, i) for i in range(12)]
            for fut in concurrent.futures.as_completed(futures):
                status, body = fut.result()
                if status == 201:
                    bodies.append(body)
                else:
                    raise AssertionError(f"unexpected status={status} body={body}")

        assert_true(len(bodies) == 12, f"expected 12 successes, got {len(bodies)}")
        transfer_ids = {b["transferId"] for b in bodies}
        assert_true(len(transfer_ids) == 1, f"expected one transferId, got {transfer_ids}")
        assert_true(all(b.get("state") == "PROCESSED" for b in bodies), "all PROCESSED")
        assert_true(self.balance(from_w["walletId"]) == Decimal("150.00"), "from")
        assert_true(self.balance(to_w["walletId"]) == Decimal("50.00"), "to")
        _, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        assert_true(len(from_stmt["entries"]) == 2, "funding + debited once")

    def test_concurrent_opposite_transfers_conserve_total(self) -> None:
        _, a = self.create_wallet(self.next_user_id(), "500.00")
        _, b = self.create_wallet(self.next_user_id(), "500.00")
        total_before = self.balance(a["walletId"]) + self.balance(b["walletId"])
        success = 0

        def pair(i: int) -> int:
            local = 0
            s1, _ = self.create_transfer(
                f"opp-a-b-{uuid.uuid4()}-{i}", a["walletId"], b["walletId"], "10.00"
            )
            s2, _ = self.create_transfer(
                f"opp-b-a-{uuid.uuid4()}-{i}", b["walletId"], a["walletId"], "10.00"
            )
            if s1 == 201:
                local += 1
            if s2 == 201:
                local += 1
            return local

        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            futures = [pool.submit(pair, i) for i in range(10)]
            for fut in concurrent.futures.as_completed(futures):
                success += fut.result()

        assert_true(success == 20, f"success={success}")
        total_after = self.balance(a["walletId"]) + self.balance(b["walletId"])
        assert_true(total_after == total_before, f"conserved {total_before} vs {total_after}")
        assert_true(self.balance(a["walletId"]) >= 0, "a non-negative")
        assert_true(self.balance(b["walletId"]) >= 0, "b non-negative")

    # --- Safe state transitions ---

    def test_success_replay_keeps_success_state(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "300.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        key = f"state-success-{uuid.uuid4()}"
        s1, first = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "40.00")
        assert_true(s1 == 201 and first.get("state") == "PROCESSED", f"first={s1}/{first}")
        s2, replay = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "40.00")
        assert_true(s2 == 201 and replay.get("state") == "PROCESSED", f"replay={s2}/{replay}")
        assert_true(replay["transferId"] == first["transferId"], "same transfer")
        assert_true(self.balance(from_w["walletId"]) == Decimal("260.00"), "from")
        assert_true(self.balance(to_w["walletId"]) == Decimal("40.00"), "to")

    def test_failed_stays_failed_until_funded_retry(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "20.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        key = f"state-failed-{uuid.uuid4()}"
        s1, body1 = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(s1 == 400 and body1.get("errorCode") == "INSUFFICIENT_BALANCE", f"{s1}/{body1}")
        s2, body2 = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(s2 == 400 and body2.get("errorCode") == "INSUFFICIENT_BALANCE", f"{s2}/{body2}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("20.00"), "unchanged")
        _, stmt = self.get_statement(wallet_id=from_w["walletId"])
        assert_true(len(stmt["entries"]) == 1, "only funding while failed")
        assert_true(stmt["entries"][0].get("transferId") is None, "funding only")

    # --- Retry-safe behavior ---

    def test_retry_after_topup_then_replay_is_idempotent(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "30.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        _, funder = self.create_wallet(self.next_user_id(), "1000.00")
        key = f"retry-safe-{uuid.uuid4()}"
        s1, body1 = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(s1 == 400 and body1.get("errorCode") == "INSUFFICIENT_BALANCE", f"{s1}/{body1}")

        fund_status, _ = self.create_transfer(
            f"fund-{uuid.uuid4()}", funder["walletId"], from_w["walletId"], "500.00"
        )
        assert_true(fund_status == 201, f"fund={fund_status}")

        s2, retried = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(s2 == 201 and retried.get("state") == "PROCESSED", f"{s2}/{retried}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("430.00"), "from after retry")
        assert_true(self.balance(to_w["walletId"]) == Decimal("100.00"), "to after retry")

        s3, replay = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "100.00")
        assert_true(s3 == 201 and replay["transferId"] == retried["transferId"], f"{s3}/{replay}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("430.00"), "no double debit")
        assert_true(self.balance(to_w["walletId"]) == Decimal("100.00"), "to stable")

    def test_concurrent_retries_after_topup_succeed_once(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "10.00")
        _, to_w = self.create_wallet(self.next_user_id(), "0.00")
        _, funder = self.create_wallet(self.next_user_id(), "1000.00")
        key = f"retry-concurrent-{uuid.uuid4()}"
        s1, body1 = self.create_transfer(key, from_w["walletId"], to_w["walletId"], "80.00")
        assert_true(s1 == 400 and body1.get("errorCode") == "INSUFFICIENT_BALANCE", f"{s1}/{body1}")

        fund_status, _ = self.create_transfer(
            f"fund-{uuid.uuid4()}", funder["walletId"], from_w["walletId"], "200.00"
        )
        assert_true(fund_status == 201, f"fund={fund_status}")

        bodies: list[dict] = []

        def one(_: int) -> tuple[int, dict]:
            return self.create_transfer(key, from_w["walletId"], to_w["walletId"], "80.00")

        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            futures = [pool.submit(one, i) for i in range(8)]
            for fut in concurrent.futures.as_completed(futures):
                status, body = fut.result()
                assert_true(status == 201, f"expected 201, got {status}: {body}")
                bodies.append(body)

        transfer_ids = {b["transferId"] for b in bodies}
        assert_true(len(transfer_ids) == 1, f"ids={transfer_ids}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("130.00"), "from")
        assert_true(self.balance(to_w["walletId"]) == Decimal("80.00"), "to")

    # --- Correct balance tracking ---

    def test_multihop_balances_and_statements(self) -> None:
        _, a = self.create_wallet(self.next_user_id(), "1000.00")
        _, b = self.create_wallet(self.next_user_id(), "100.00")
        _, c = self.create_wallet(self.next_user_id(), "50.00")
        total_before = self.balance(a["walletId"]) + self.balance(b["walletId"]) + self.balance(c["walletId"])

        assert_true(
            self.create_transfer(f"bal-ab-{uuid.uuid4()}", a["walletId"], b["walletId"], "200.00")[0]
            == 201,
            "a->b",
        )
        assert_true(
            self.create_transfer(f"bal-bc-{uuid.uuid4()}", b["walletId"], c["walletId"], "75.00")[0]
            == 201,
            "b->c",
        )
        assert_true(
            self.create_transfer(f"bal-ca-{uuid.uuid4()}", c["walletId"], a["walletId"], "25.00")[0]
            == 201,
            "c->a",
        )

        assert_true(self.balance(a["walletId"]) == Decimal("825.00"), "a")
        assert_true(self.balance(b["walletId"]) == Decimal("225.00"), "b")
        assert_true(self.balance(c["walletId"]) == Decimal("100.00"), "c")
        total_after = self.balance(a["walletId"]) + self.balance(b["walletId"]) + self.balance(c["walletId"])
        assert_true(total_after == total_before == Decimal("1150.00"), "conserved")

        _, stmt_a = self.get_statement(wallet_id=a["walletId"])
        _, stmt_b = self.get_statement(wallet_id=b["walletId"])
        _, stmt_c = self.get_statement(wallet_id=c["walletId"])
        assert_true(dec(stmt_a["currentBalance"]) == self.balance(a["walletId"]), "stmt a")
        assert_true(dec(stmt_b["currentBalance"]) == self.balance(b["walletId"]), "stmt b")
        assert_true(dec(stmt_c["currentBalance"]) == self.balance(c["walletId"]), "stmt c")
        assert_true(dec(stmt_a["entries"][0]["balanceAfterTransfer"]) == Decimal("1000.00"), "a funding")
        assert_true(dec(stmt_a["entries"][1]["balanceAfterTransfer"]) == Decimal("800.00"), "a0")
        assert_true(dec(stmt_a["entries"][2]["balanceAfterTransfer"]) == Decimal("825.00"), "a1")
        assert_true(dec(stmt_b["entries"][0]["balanceAfterTransfer"]) == Decimal("100.00"), "b funding")
        assert_true(dec(stmt_b["entries"][1]["balanceAfterTransfer"]) == Decimal("300.00"), "b0")
        assert_true(dec(stmt_b["entries"][2]["balanceAfterTransfer"]) == Decimal("225.00"), "b1")
        assert_true(dec(stmt_c["entries"][0]["balanceAfterTransfer"]) == Decimal("50.00"), "c funding")
        assert_true(dec(stmt_c["entries"][1]["balanceAfterTransfer"]) == Decimal("125.00"), "c0")
        assert_true(dec(stmt_c["entries"][2]["balanceAfterTransfer"]) == Decimal("100.00"), "c1")

    def test_failed_transfer_leaves_balances_and_statements_untouched(self) -> None:
        _, from_w = self.create_wallet(self.next_user_id(), "55.00")
        _, to_w = self.create_wallet(self.next_user_id(), "10.00")
        status, body = self.create_transfer(
            f"bal-fail-{uuid.uuid4()}", from_w["walletId"], to_w["walletId"], "999.00"
        )
        assert_true(status == 400 and body.get("errorCode") == "INSUFFICIENT_BALANCE", f"{status}/{body}")
        assert_true(self.balance(from_w["walletId"]) == Decimal("55.00"), "from")
        assert_true(self.balance(to_w["walletId"]) == Decimal("10.00"), "to")
        _, from_stmt = self.get_statement(wallet_id=from_w["walletId"])
        _, to_stmt = self.get_statement(wallet_id=to_w["walletId"])
        assert_true(len(from_stmt["entries"]) == 1 and from_stmt["entries"][0].get("transferId") is None, "from funding only")
        assert_true(len(to_stmt["entries"]) == 1 and to_stmt["entries"][0].get("transferId") is None, "to funding only")


class SkipTest(Exception):
    pass


CASES: list[tuple[str, str, str, Callable[[Suite], None]]] = [
    (
        "createWallet_persistsAndReturnsCreatedWallet",
        "POST /wallets",
        "Create wallet and verify response + persisted balance via statement",
        Suite.test_create_wallet,
    ),
    (
        "createWallet_whenUserAlreadyHasWallet_returnsConflict",
        "POST /wallets",
        "Second wallet for same userId returns 409 WALLET_ALREADY_EXISTS",
        Suite.test_duplicate_wallet,
    ),
    (
        "createWallet_whenBalanceNegative_returnsBadRequest",
        "POST /wallets",
        "Negative balance rejected with 400 VALIDATION_ERROR",
        Suite.test_negative_balance,
    ),
    (
        "createTransfer_movesFundsWritesLedgerAndMarksSuccess",
        "POST /transfers (+ GET /statements)",
        "Happy-path transfer updates balances and creates ledger entries",
        Suite.test_transfer_success,
    ),
    (
        "createTransfer_whenWalletMissing_returnsNotFound",
        "POST /transfers",
        "Unknown destination wallet returns 404 WALLETS_NOT_FOUND",
        Suite.test_wallet_missing,
    ),
    (
        "createTransfer_whenInsufficientBalance_marksFailedAndLeavesBalancesUnchanged",
        "POST /transfers (+ GET /statements)",
        "Insufficient funds returns 400; balances and ledger unchanged",
        Suite.test_insufficient_balance,
    ),
    (
        "createTransfer_whenSameIdempotencyKeyAndPayload_returnsSameTransferWithoutDoubleDebit",
        "POST /transfers",
        "Idempotent replay returns same transferId without double debit",
        Suite.test_idempotent_replay,
    ),
    (
        "createTransfer_whenSameIdempotencyKeyDifferentPayload_returnsConflict",
        "POST /transfers",
        "Same idempotency key with different amount returns 409",
        Suite.test_idempotency_conflict,
    ),
    (
        "createTransfer_whenPreviouslyFailed_retriesAndSucceedsAfterTopUp",
        "POST /transfers",
        "FAILED transfer retries to PROCESSED after API top-up from funding wallet",
        Suite.test_failed_then_retry_after_topup,
    ),
    (
        "createTransfer_whenAmountInvalid_returnsValidationError",
        "POST /transfers",
        "Amount 0.00 rejected with 400 VALIDATION_ERROR",
        Suite.test_invalid_amount,
    ),
    (
        "createTransfer_whenWalletsAreSame_returnsValidationError",
        "POST /transfers",
        "Same from/to wallet rejected with 400 VALIDATION_ERROR",
        Suite.test_same_wallet,
    ),
    (
        "getStatement_byWalletId_afterTransfer_showsRunningBalances",
        "GET /statements?walletId=",
        "Statement shows DEBIT/CREDIT with correct running balances",
        Suite.test_statement_after_transfer,
    ),
    (
        "getStatement_byUserId_returnsWalletStatement",
        "GET /statements?userId=",
        "Lookup statement by userId for a single wallet",
        Suite.test_statement_by_user,
    ),
    (
        "getStatement_whenNeitherParamProvided_returnsBadRequest",
        "GET /statements",
        "Missing query params returns 400 VALIDATION_ERROR",
        Suite.test_statement_missing_params,
    ),
    (
        "getStatement_whenWalletMissing_returnsNotFound",
        "GET /statements?walletId=",
        "Unknown walletId returns 404 WALLET_NOT_FOUND",
        Suite.test_statement_wallet_missing,
    ),
    (
        "oneWalletPerUser_secondCreateRejected_statementStillWorks",
        "POST /wallets, GET /statements?userId=",
        "Second wallet for same userId returns 409; statement by userId still works",
        Suite.test_one_wallet_per_user_enforced,
    ),
    (
        "getStatement_whenWalletDoesNotBelongToUser_returnsBadRequest",
        "GET /statements?userId=&walletId=",
        "Mismatched userId/walletId returns 400 WALLET_USER_MISMATCH",
        Suite.test_statement_user_mismatch,
    ),
    (
        "getStatement_whenBothParamsMatch_returnsOk",
        "GET /statements?userId=&walletId=",
        "Matching userId + walletId returns 200 statement",
        Suite.test_statement_both_params,
    ),
    (
        "concurrentTransfers_doNotOverdrawSourceWallet",
        "POST /transfers (parallel)",
        "20 parallel transfers of 10 from balance 100: exactly 10 success, no overdraft",
        Suite.test_concurrent_no_overdraw,
    ),
    (
        "endToEnd_createWalletsTransferAndReadStatements",
        "POST /wallets, POST /transfers, GET /statements",
        "Two wallets, two transfers, statement chronological balances",
        Suite.test_end_to_end,
    ),
    (
        "concurrentIdenticalIdempotencyKey_createsSingleTransferAndDebitsOnce",
        "POST /transfers (parallel, same key)",
        "Concurrent identical idempotency key yields one PROCESSED transfer and one debit",
        Suite.test_concurrent_same_idempotency_key,
    ),
    (
        "concurrentOppositeDirectionTransfers_conserveTotalBalance",
        "POST /transfers (parallel A↔B)",
        "Opposite-direction concurrent transfers conserve total funds",
        Suite.test_concurrent_opposite_transfers_conserve_total,
    ),
    (
        "processedTransfer_replayKeepsProcessedAndDoesNotChangeStateOrBalances",
        "POST /transfers",
        "PROCESSED replay stays PROCESSED and does not change balances",
        Suite.test_success_replay_keeps_success_state,
    ),
    (
        "failedTransfer_remainsFailedUntilSuccessfulRetry",
        "POST /transfers",
        "FAILED stays FAILED (and balances unchanged) until a funded retry succeeds",
        Suite.test_failed_stays_failed_until_funded_retry,
    ),
    (
        "retryAfterTopUp_isIdempotentOnFurtherReplays",
        "POST /transfers",
        "After FAILED→PROCESSED retry, further replays are idempotent",
        Suite.test_retry_after_topup_then_replay_is_idempotent,
    ),
    (
        "concurrentRetriesOfFailedKey_afterTopUp_succeedOnce",
        "POST /transfers (parallel retry)",
        "Concurrent retries of a FAILED key after top-up succeed once",
        Suite.test_concurrent_retries_after_topup_succeed_once,
    ),
    (
        "multiHopTransfers_statementBalancesMatchWalletAndConserveFunds",
        "POST /transfers, GET /statements",
        "Multi-hop transfers conserve funds; statements match wallet balances",
        Suite.test_multihop_balances_and_statements,
    ),
    (
        "failedTransfer_doesNotAffectBalancesOrStatementEntries",
        "POST /transfers, GET /statements",
        "Failed transfer leaves balances and statement entries untouched",
        Suite.test_failed_transfer_leaves_balances_and_statements_untouched,
    ),
]


def wait_for_service(client: ApiClient, timeout_sec: float = 90.0) -> None:
    deadline = time.time() + timeout_sec
    last_err = None
    while time.time() < deadline:
        try:
            # Any response (including 400) means the server is up.
            client.request("GET", "/statements")
            return
        except Exception as exc:  # noqa: BLE001
            last_err = exc
            time.sleep(1.0)
    raise RuntimeError(f"Service not reachable at {client.base_url}: {last_err}")


def run_suite(base_url: str) -> Report:
    client = ApiClient(base_url)
    wait_for_service(client)
    suite = Suite(client)
    report = Report(
        base_url=base_url,
        started_at=datetime.now(timezone.utc).isoformat(),
    )
    for name, api, summary, fn in CASES:
        started = time.time()
        try:
            fn(suite)
            report.results.append(
                TestResult(
                    name=name,
                    api=api,
                    summary=summary,
                    status="PASS",
                    duration_ms=int((time.time() - started) * 1000),
                )
            )
        except SkipTest as exc:
            report.results.append(
                TestResult(
                    name=name,
                    api=api,
                    summary=summary,
                    status="SKIP",
                    detail=str(exc),
                    duration_ms=int((time.time() - started) * 1000),
                )
            )
        except Exception as exc:  # noqa: BLE001
            report.results.append(
                TestResult(
                    name=name,
                    api=api,
                    summary=summary,
                    status="FAIL",
                    detail=f"{exc}\n{traceback.format_exc()}",
                    duration_ms=int((time.time() - started) * 1000),
                )
            )
    report.finished_at = datetime.now(timezone.utc).isoformat()
    return report


def render_markdown(report: Report) -> str:
    lines = [
        "# API Integration Test Report",
        "",
        f"- **Base URL:** `{report.base_url}`",
        f"- **Started:** {report.started_at}",
        f"- **Finished:** {report.finished_at}",
        f"- **Totals:** {report.passed} passed, {report.failed} failed, {report.skipped} skipped "
        f"(of {len(report.results)})",
        "",
        "| # | Test case | API | Summary | Result | Duration |",
        "|---|---|---|---|---|---|",
    ]
    for idx, r in enumerate(report.results, start=1):
        summary = r.summary.replace("|", "\\|")
        lines.append(
            f"| {idx} | `{r.name}` | `{r.api}` | {summary} | **{r.status}** | {r.duration_ms} ms |"
        )
    lines.append("")
    failures = [r for r in report.results if r.status == "FAIL"]
    skips = [r for r in report.results if r.status == "SKIP"]
    if failures:
        lines.append("## Failures")
        lines.append("")
        for r in failures:
            lines.append(f"### `{r.name}`")
            lines.append("")
            lines.append("```")
            lines.append(r.detail.strip())
            lines.append("```")
            lines.append("")
    if skips:
        lines.append("## Skipped")
        lines.append("")
        for r in skips:
            lines.append(f"- `{r.name}`: {r.detail}")
        lines.append("")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run API integration tests against a live service")
    parser.add_argument(
        "--base-url",
        default="http://127.0.0.1:8080",
        help="Service base URL (default: http://127.0.0.1:8080)",
    )
    parser.add_argument(
        "--report",
        default=str(Path("reports") / "api_integration_report.md"),
        help="Markdown report output path",
    )
    args = parser.parse_args()

    report = run_suite(args.base_url)
    markdown = render_markdown(report)
    out = Path(args.report)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(markdown, encoding="utf-8")
    print(markdown)
    print(f"\nReport written to {out.resolve()}", file=sys.stderr)
    return 1 if report.failed else 0


if __name__ == "__main__":
    sys.exit(main())
