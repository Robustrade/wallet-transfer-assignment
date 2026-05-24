import express from 'express';
import { Pool } from 'pg';
import { WalletRepository } from './repository/wallet.repository';
import { TransferRepository } from './repository/transfer.repository';
import { LedgerRepository } from './repository/ledger.repository';
import { TransferService } from './service/transfer.service';
import { createTransferRouter } from './handler/transfer.handler';
import { createWalletRouter } from './handler/wallet.handler';
import { errorHandler } from './middleware/error-handler';

export function createApp(pool: Pool) {
  const walletRepo = new WalletRepository(pool);
  const transferRepo = new TransferRepository(pool);
  const ledgerRepo = new LedgerRepository(pool);
  const transferService = new TransferService(pool, walletRepo, transferRepo, ledgerRepo);

  const app = express();
  app.use(express.json());

  app.get('/health', (_req, res) => res.json({ status: 'ok' }));
  app.use('/transfers', createTransferRouter(transferService));
  app.use('/wallets', createWalletRouter(walletRepo, ledgerRepo));

  app.use(errorHandler);

  return app;
}
