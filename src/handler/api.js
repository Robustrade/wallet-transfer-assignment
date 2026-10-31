// HTTP transport: parse request shape, call the service, map results and errors to HTTP.
import http from 'node:http';
import { TransferStatus, ValidationError } from '../domain/index.js';

const MAX_BODY_BYTES = 1024 * 1024;

const ERROR_STATUS = Object.freeze({
  VALIDATION_ERROR: 400,
  WALLET_NOT_FOUND: 404,
  TRANSFER_NOT_FOUND: 404,
  WALLET_ALREADY_EXISTS: 409,
  IDEMPOTENCY_KEY_CONFLICT: 409,
  INVALID_STATE_TRANSITION: 409,
});

// ---- serializers (transport mapping) -------------------------------------------------------
const transferToJson = (t) => ({
  id: t.id, idempotencyKey: t.idempotencyKey, fromWalletId: t.fromWalletId, toWalletId: t.toWalletId,
  amount: t.amount, status: t.status, failureReason: t.failureReason, createdAt: t.createdAt, updatedAt: t.updatedAt,
});
const walletToJson = (w) => ({ id: w.id, balance: w.balance, createdAt: w.createdAt });

// ---- helpers ---------------------------------------------------------------------------------
function send(res, status, body, headers = {}) {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
    ...headers,
  });
  res.end(payload);
}

const sendError = (res, code, message, status) => send(res, status, { error: { code, message } });

function readJsonObject(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        reject(new ValidationError('request body too large'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('error', reject);
    req.on('end', () => {
      try {
        const parsed = JSON.parse(Buffer.concat(chunks).toString('utf8'));
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object');
        resolve(parsed);
      } catch {
        reject(new ValidationError('request body must be a JSON object'));
      }
    });
  });
}

// Retryable PostgreSQL conditions: nothing was committed, so the client may retry safely
// (idempotency keys make that retry exactly-once).
//   40001 serialization_failure, 40P01 deadlock_detected, 55P03 lock_not_available (lock_timeout),
//   57014 query_canceled, 53300 too_many_connections; plus connection-level failures.
const RETRYABLE_PG_CODES = new Set(['40001', '40P01', '55P03', '57014', '53300']);
const RETRYABLE_NET_CODES = new Set(['ECONNREFUSED', 'ECONNRESET', 'ETIMEDOUT']);
const isDbBusy = (err) => RETRYABLE_PG_CODES.has(err?.code) || RETRYABLE_NET_CODES.has(err?.code);

/** Builds (but does not start) the HTTP server. */
export function createApp(service, { logger = console } = {}) {
  const routes = [
    ['POST', /^\/transfers$/, async (req, res) => {
      const body = await readJsonObject(req);
      const { transfer, replayed } = await service.createTransfer({
        fromWalletId: body.fromWalletId, toWalletId: body.toWalletId,
        amount: body.amount, idempotencyKey: body.idempotencyKey ?? null,
      });
      // FAILED is 422 for the first call and for replays; otherwise 201 (new) / 200 (replay)
      const status = transfer.status === TransferStatus.FAILED ? 422 : replayed ? 200 : 201;
      send(res, status, transferToJson(transfer), replayed ? { 'Idempotent-Replayed': 'true' } : {});
    }],
    ['GET', /^\/transfers\/([^/]+)$/, async (req, res, [id]) => send(res, 200, transferToJson(await service.getTransfer(id)))],
    ['POST', /^\/wallets$/, async (req, res) => {
      const body = await readJsonObject(req);
      send(res, 201, walletToJson(await service.createWallet(body.id, body.initialBalance ?? 0)));
    }],
    ['GET', /^\/wallets\/([^/]+)$/, async (req, res, [id]) => send(res, 200, walletToJson(await service.getWallet(id)))],
    ['GET', /^\/wallets\/([^/]+)\/ledger$/, async (req, res, [id]) => send(res, 200, await service.getLedger(id))],
  ];

  return http.createServer(async (req, res) => {
    try {
      const { pathname } = new URL(req.url, 'http://localhost');
      let pathMatched = false;
      for (const [method, pattern, handler] of routes) {
        const match = pattern.exec(pathname);
        if (!match) continue;
        pathMatched = true;
        if (method !== req.method) continue;
        return await handler(req, res, match.slice(1).map(decodeURIComponent));
      }
      return pathMatched
        ? sendError(res, 'METHOD_NOT_ALLOWED', 'method not allowed', 405)
        : sendError(res, 'NOT_FOUND', 'route not found', 404);
    } catch (err) {
      if (err?.code in ERROR_STATUS) return sendError(res, err.code, err.message, ERROR_STATUS[err.code]);
      if (isDbBusy(err)) return sendError(res, 'SERVICE_UNAVAILABLE', 'temporarily unavailable, retry', 503);
      logger.error?.('unhandled error', err);
      return sendError(res, 'INTERNAL_ERROR', 'internal server error', 500);
    }
  });
}
