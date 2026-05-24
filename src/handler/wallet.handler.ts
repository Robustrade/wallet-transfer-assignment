import { Router, Request, Response, NextFunction } from 'express';
import { WalletRepository } from '../repository/wallet.repository';
import { LedgerRepository } from '../repository/ledger.repository';

export function createWalletRouter(wallets: WalletRepository, ledger: LedgerRepository): Router {
  const router = Router();

  router.get('/:id', async (req: Request, res: Response, next: NextFunction) => {
    try {
      const wallet = await wallets.findById(req.params.id);
      if (!wallet) return res.status(404).json({ error: 'Wallet not found' });
      return res.json(wallet);
    } catch (err) {
      return next(err);
    }
  });

  router.get('/:id/ledger', async (req: Request, res: Response, next: NextFunction) => {
    try {
      const entries = await ledger.findByWalletId(req.params.id);
      return res.json(entries);
    } catch (err) {
      return next(err);
    }
  });

  return router;
}
