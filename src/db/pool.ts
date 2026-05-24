import { Pool } from 'pg';

const connectionString =
  process.env.NODE_ENV === 'test'
    ? process.env.TEST_DATABASE_URL ||
      'postgresql://wallet:wallet@localhost:5433/wallet_transfer_test'
    : process.env.DATABASE_URL || 'postgresql://wallet:wallet@localhost:5432/wallet_transfer';

export const pool = new Pool({ connectionString });
