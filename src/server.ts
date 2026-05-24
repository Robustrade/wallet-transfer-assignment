import { pool } from './db/pool';
import { runMigrations } from './db/migrate';
import { createApp } from './app';

const PORT = process.env.PORT ? parseInt(process.env.PORT, 10) : 3000;

async function main() {
  await runMigrations(pool);
  const app = createApp(pool);
  app.listen(PORT, () => {
    console.log(`Wallet Transfer Service listening on port ${PORT}`);
  });
}

main().catch((err) => {
  console.error('Failed to start server:', err);
  process.exit(1);
});
