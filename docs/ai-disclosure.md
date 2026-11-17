# AI disclosure

## Tool and working style

1. **Tool:** Cursor (Agent mode), Composer coding model.
2. **How I use it:** documentation-first planning, schema and design debate, then incremental implementation and review. I set the design tradeoffs; AI helps draft, refactor, and reason through options like locking and idempotent failure handling.
3. **Evidence:** representative high-value prompts below (paraphrased / shortened). Full local transcripts stay in Cursor; they are not checked into the repo.

## Representative interactions

These are the kinds of prompts that actually shaped the design—not “write the whole service” dumps.

### Planning and concurrency

- Create an implementation plan for the assignment with focus on idempotency, concurrency, ledger consistency, and clean layering.
- Walk through two concurrent debits on the same wallet: how row locks behave if both transactions start together; whether Serializable isolation is required.
- Compare Postgres Read Committed + ordered `FOR UPDATE` vs an in-process Go mutex for money safety across processes and tests.

### Schema and books

- Design tables for idempotent transfers and ACID retries; start with single-currency INR, keep room to extend later.
- Challenge columns and constraints that did not earn their keep (`signed_amount`, `processed_at`, hash/body CHECK clutter, redundant idempotency→transfer pointer).
- Clarify why `opening_balance` exists outside the ledger, and keep only the triggers needed for immutability + balance↔ledger equation.

### Architecture and layering

- Keep handler / service / repository / model separation; push business rules into the service; leave repositories SQL-only.
- Prefer named SQL parameters; ask whether GORM/goose are needed (kept a small custom migrator).

### Correctness hardening

- Persist and replay terminal insufficient-funds outcomes so the same key stays sticky after funds arrive.
- Add overflow guard before credit; separate test DB from hand-used API DB; concurrency cases for contended / opposite / disjoint wallet pairs.
- Refresh `docs/design.md` to match the decisions above.

### Future scale: brokers, saga, sharding

- Explore extending the sync transfer path with message brokers (Kafka / RabbitMQ) so debit/credit steps can move to async processing later.
- Discuss a saga pattern for multi-step / cross-boundary transfers while keeping local money moves transactional and idempotent.
- Ask how row locks + single-DB transactions adapt under sharded databases (shard-local wallets, cross-shard saga, per-shard idempotency).

## What was not dumped here

Routine “implement this to-do”, paste-the-assignment, “run the tests”, and short clarifying pings are omitted on purpose. Reviewers can ask for more detail in discussion if needed.
