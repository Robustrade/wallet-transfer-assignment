# For Interviewers/Reviewers

## Run Everything with One Command

```bash
./quickstart.sh
```

**That's it!** This will:
- ✅ Setup PostgreSQL database
- ✅ Build the application
- ✅ Start the server
- ✅ Run 8 automated tests
- ✅ Verify all production features

**Time:** ~2 minutes

---

## What You Get

### Automated Tests (8 tests)
All tests run automatically:
1. ✅ Health check
2. ✅ Successful transfer
3. ✅ Idempotency (duplicate requests)
4. ✅ Request hash validation
5. ✅ Validation errors NOT cached
6. ✅ Wallet not found NOT cached
7. ✅ Double-entry ledger integrity
8. ✅ Transfer tracking

### Test Output
Colored output with ✓/✗ for each test, plus:
- Balance changes
- Transfer IDs
- Database verification
- Ledger integrity checks

---

## File Organization

```
📁 wallet-transfer-assignment/
│
├── 🚀 quickstart.sh           # ← START HERE (one command)
├── 📖 QUICKSTART_GUIDE.md      # Detailed guide for reviewers
├── 📖 REVIEWER_README.md       # This file
├── 📖 README.md                # Project overview
├── 📖 TESTING.md               # Manual testing guide
│
├── 📁 scripts/                 # All automation scripts
│   ├── setup.sh                # Database setup
│   ├── run.sh                  # Start server
│   ├── test.sh                 # Automated tests
│   ├── seed_data.sql           # Test data
│   └── README.md               # Script docs
│
├── 📁 docs/                    # Documentation
│   └── schema.md               # Database design
│
├── 📁 cmd/server/              # Application entry point
├── 📁 internal/                # Core implementation
│   ├── handlers/               # HTTP layer
│   ├── services/               # Business logic ⭐
│   ├── repositories/           # Data access
│   └── models/                 # Domain models
│
└── 📁 migrations/              # Database migrations
```

---

## Default Test Data

After running `quickstart.sh`, you get:

| Wallet ID | Initial Balance |
|-----------|----------------|
| wallet_1  | $1000.00       |
| wallet_2  | $500.00        |

**Customize:** Edit `scripts/seed_data.sql`

---

## Manual Testing (Optional)

Server stays running after `quickstart.sh`. Test manually:

```bash
# Transfer $50
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "my-test-001",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 50.00
  }'

# Check balances
psql -U postgres -d wallet_transfer -c "SELECT * FROM wallets;"
```

More examples in [TESTING.md](TESTING.md)

---

## Key Code Sections to Review

### Production Features
1. **Atomic Idempotency** - `internal/services/transfer_service.go:58-97`
   - INSERT ON CONFLICT inside transaction
   - Two-transaction approach for duplicate handling

2. **Deadlock Prevention** - `internal/services/transfer_service.go:99-101`
   - Deterministic lock ordering
   - Sorted wallet IDs

3. **Smart Caching** - `internal/services/transfer_service.go:118-122`
   - Validation errors NOT cached
   - Allows retry after fixing issues

4. **PROCESSING Timeout** - `internal/services/transfer_service.go:207-212`
   - 30-second timeout for crashed requests
   - Prevents stuck transactions

5. **Connection Pool** - `cmd/server/main.go:33-35`
   - MaxOpenConns=25
   - MaxIdleConns=5
   - ConnMaxLifetime=5min

### Database Design
See `docs/schema.md` for:
- Complete schema documentation
- Concurrency strategy
- Double-entry accounting
- Idempotency design

---

## Documentation

| File | Purpose |
|------|---------|
| **QUICKSTART_GUIDE.md** | Detailed quick start for reviewers |
| **README.md** | Project overview and features |
| **TESTING.md** | Manual testing guide with curl examples |
| **docs/schema.md** | Database design and concurrency |
| **scripts/README.md** | Script documentation |
| **SETUP.md** | Detailed setup instructions |

---

## Production Considerations

### What's Implemented ✅
- Atomic idempotency (database constraints)
- Request hash validation
- PROCESSING timeout (30s)
- Validation errors NOT cached
- Connection pool configuration
- Deterministic locking
- Double-entry ledger

### Known Limitations (Documented)
- Two-transaction approach for duplicate checks
- No context.Context (would add for production)
- No FOR UPDATE on duplicate reads

See **"Production Considerations"** in README.md for full details.

---

## Stop the Server

```bash
# PID shown in quickstart output
kill <PID>

# Or find it
lsof -ti:8080 | xargs kill
```

---

## Troubleshooting

### PostgreSQL not running
```bash
brew services start postgresql  # macOS
sudo systemctl start postgresql # Linux
```

### Port already in use
```bash
lsof -ti:8080 | xargs kill
```

### Start fresh
```bash
psql -U postgres -c "DROP DATABASE wallet_transfer;"
./quickstart.sh
```

---

## Questions?

All design decisions are documented with rationale. See:
- Production features: README.md (Production Considerations section)
- Database design: docs/schema.md
- Testing approach: TESTING.md
- Trade-offs: README.md (Known Limitations section)

---

**Enjoy reviewing! The single command `./quickstart.sh` is all you need to see everything in action.**
