# Quick Start Guide

## Setup

### Prerequisites
- Go 1.21+
- PostgreSQL 12+
- Docker (optional)

### Local Development

```bash
# Clone repository
git clone <repo-url>
cd wallet-transfer

# Install dependencies
go mod download

# Setup database
# Option 1: Docker
docker-compose -f docker/docker-compose.yml up -d postgres
sleep 5

# Option 2: Manual (PostgreSQL must be running)
# Create database
createdb wallet_transfer

# Run migrations
psql wallet_transfer < migrations/001_init.sql

# Run application
go run cmd/server/main.go

# Server starts on :8080
```

### Docker Compose

```bash
# Start all services (PostgreSQL + API)
docker-compose -f docker/docker-compose.yml up -d

# View logs
docker-compose -f docker/docker-compose.yml logs -f app

# Stop services
docker-compose -f docker/docker-compose.yml down
```

## API Usage Examples

### Create Transfer

```bash
curl -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "unique-key-123",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 5000
  }'
```

### Get Transfer

```bash
curl http://localhost:8080/transfers/{transferId}
```

### Get Wallet Balance

```bash
curl http://localhost:8080/wallets/{walletId}
```

## Testing

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests
make test-int

# Generate coverage report
make test-cov
```

## Troubleshooting

### Connection Refused

```
error: failed to connect to database
```

**Solution:** Ensure PostgreSQL is running
```bash
docker-compose -f docker/docker-compose.yml up -d postgres
```

### Migrations Not Applied

```
error: table "transfers" does not exist
```

**Solution:** Run migrations manually
```bash
psql wallet_transfer < migrations/001_init.sql
```

### Port Already in Use

```
error: listen tcp :8080: bind: address already in use
```

**Solution:** Change port
```bash
SERVER_ADDR=:8081 go run cmd/server/main.go
```
