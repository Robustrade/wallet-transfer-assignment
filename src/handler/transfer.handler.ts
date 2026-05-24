import { Router, Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { TransferService } from '../service/transfer.service';
import { ValidationError } from '../domain/errors';

const createTransferSchema = z.object({
  idempotencyKey: z.string().min(1).max(255),
  fromWalletId: z.string().min(1),
  toWalletId: z.string().min(1),
  amount: z.number().positive(),
});

export function createTransferRouter(service: TransferService): Router {
  const router = Router();

  router.post('/', async (req: Request, res: Response, next: NextFunction) => {
    const parsed = createTransferSchema.safeParse(req.body);
    if (!parsed.success) {
      return next(new ValidationError('Invalid request body', parsed.error.format()));
    }

    try {
      const result = await service.createTransfer(parsed.data);
      const statusCode = result.duplicate ? 200 : result.transfer.status === 'FAILED' ? 422 : 201;
      return res.status(statusCode).json(result);
    } catch (err) {
      return next(err);
    }
  });

  return router;
}
