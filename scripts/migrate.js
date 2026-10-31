// Creates/updates the schema. Idempotent. Usage: npm run migrate
import { DEFAULT_DATABASE_URL } from '../src/main.js';
import { Database } from '../src/repository/index.js';

const db = new Database(process.env.DATABASE_URL ?? DEFAULT_DATABASE_URL);
try {
  await db.initSchema();
  console.log('schema is up to date');
} finally {
  await db.close();
}
