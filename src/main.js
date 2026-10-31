// Composition root: wires database -> service -> HTTP server.
import { fileURLToPath } from 'node:url';
import { createApp } from './handler/index.js';
import { Database } from './repository/index.js';
import { TransferService } from './service/index.js';

export const DEFAULT_DATABASE_URL = 'postgres://wallet:wallet@localhost:5432/wallet';
const stdoutLogger = { info: (line) => console.log(`${new Date().toISOString()} INFO ${line}`) };

export async function buildServer(databaseUrl = process.env.DATABASE_URL ?? DEFAULT_DATABASE_URL) {
  const db = new Database(databaseUrl);
  await db.initSchema();
  const server = createApp(new TransferService(db, { logger: stdoutLogger }));
  server.on('close', () => { db.close().catch(() => {}); });
  return server;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const port = Number(process.env.PORT ?? 8000);
  const server = await buildServer();
  server.listen(port, () => console.log(`wallet-transfer listening on http://localhost:${port}`));
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.close(() => process.exit(0)));
}
