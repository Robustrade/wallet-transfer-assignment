import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  InvalidStateTransition, Transfer, TransferStatus, ValidationError, requestFingerprint,
  validateTransferRequest,
} from '../src/domain/index.js';

const pending = () => Transfer.newPending({ fromWalletId: 'a', toWalletId: 'b', amount: 5, idempotencyKey: null, now: 't0' });

describe('transfer state machine', () => {
  it('PENDING can become PROCESSED', () => {
    assert.equal(pending().transitionTo(TransferStatus.PROCESSED, 't1').status, 'PROCESSED');
  });

  it('PENDING can become FAILED with a reason', () => {
    const t = pending().transitionTo(TransferStatus.FAILED, 't1', 'INSUFFICIENT_FUNDS');
    assert.deepEqual([t.status, t.failureReason], ['FAILED', 'INSUFFICIENT_FUNDS']);
  });

  it('terminal states cannot change', () => {
    for (const [terminal, reason] of [['PROCESSED', null], ['FAILED', 'X']]) {
      const t = pending().transitionTo(terminal, 't1', reason);
      for (const target of Object.values(TransferStatus)) {
        assert.throws(() => t.transitionTo(target, 't2', target === 'FAILED' ? 'X' : null), InvalidStateTransition);
      }
    }
  });

  it('FAILED requires a reason; PROCESSED forbids one', () => {
    assert.throws(() => pending().transitionTo('FAILED', 't1'), InvalidStateTransition);
    assert.throws(() => pending().transitionTo('PROCESSED', 't1', 'oops'), InvalidStateTransition);
  });

  it('transfers are immutable', () => {
    const t = pending();
    assert.throws(() => { t.amount = 999; }, TypeError);
    t.transitionTo('PROCESSED', 't1');
    assert.equal(t.status, 'PENDING');
  });
});

describe('request validation', () => {
  const base = { fromWalletId: 'a', toWalletId: 'b', amount: 1, idempotencyKey: 'key' };

  it('accepts a valid request (key optional)', () => {
    validateTransferRequest(base);
    validateTransferRequest({ ...base, idempotencyKey: undefined });
  });

  it('rejects bad input', () => {
    const bad = [
      { toWalletId: 'a' }, { amount: 0 }, { amount: -5 }, { amount: 1.5 }, { amount: '10' },
      { amount: true }, { amount: NaN }, { amount: Infinity }, { amount: 10 ** 16 },
      { amount: 2 ** 60 }, { fromWalletId: '' }, { fromWalletId: null }, { fromWalletId: 7 },
      { idempotencyKey: '' }, { idempotencyKey: 'k'.repeat(256) }, { idempotencyKey: 42 },
    ];
    for (const override of bad) {
      assert.throws(() => validateTransferRequest({ ...base, ...override }), ValidationError, JSON.stringify(override));
    }
  });

  it('fingerprint depends on the payload', () => {
    assert.equal(requestFingerprint('a', 'b', 1), requestFingerprint('a', 'b', 1));
    assert.notEqual(requestFingerprint('a', 'b', 1), requestFingerprint('a', 'b', 2));
    assert.notEqual(requestFingerprint('a', 'b', 1), requestFingerprint('b', 'a', 1));
  });
});
