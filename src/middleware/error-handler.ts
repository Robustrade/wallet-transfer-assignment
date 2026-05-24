import { Request, Response, NextFunction } from 'express';
import { TransferError, ValidationError } from '../domain/errors';

export function errorHandler(
  err: unknown,
  _req: Request,
  res: Response,
  _next: NextFunction,
): void {
  if (err instanceof ValidationError) {
    res.status(400).json({ error: err.message, details: err.details });
    return;
  }

  if (err instanceof TransferError) {
    res.status(err.statusCode).json({ error: err.message, code: err.code });
    return;
  }

  console.error('Unhandled error:', err);
  res.status(500).json({ error: 'Internal server error' });
}
