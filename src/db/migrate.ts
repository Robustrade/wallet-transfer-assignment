import fs from 'fs';
import path from 'path';
import { Pool } from 'pg';

export async function runMigrations(pool: Pool): Promise<void> {
  const sql = fs.readFileSync(
    path.join(__dirname, '../../migrations/001_initial_schema.sql'),
    'utf-8',
  );
  await pool.query(sql);
}

// Standalone runner: ts-node src/db/migrate.ts
if (require.main === module) {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { pool } = require('./pool');
  runMigrations(pool)
    .then(() => {
      console.log('Migrations applied successfully');
      return pool.end();
    })
    .catch((err) => {
      console.error('Migration failed:', err);
      process.exit(1);
    });
}
