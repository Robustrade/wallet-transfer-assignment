# Configuration

Non-secret settings live in `config.yaml`. Secrets are environment variables. The process exits on startup when required configuration is missing or inconsistent.

## File

`WALLET_CONFIG` selects the file. The default is `config.yaml`.

```yaml
server:
  address: ":8080"
  readHeaderTimeout: 5s
  readTimeout: 15s
  writeTimeout: 15s
  idleTimeout: 60s
  shutdownTimeout: 15s
  requestTimeout: 10s

database:
  host: localhost
  port: 5432
  name: wallet
  user: wallet
  sslmode: disable
  maxConns: 20
  minConns: 2
  maxConnLifetime: 30m
  maxConnIdleTime: 5m
  connectRetries: 10
  connectRetryInterval: 1s
  lockTimeout: 5s
  statementTimeout: 8s

log:
  level: info   # debug, info, warn, error
  format: json  # json or text
```

`config.example.yaml` is the same template. `config.yaml` in the repository is safe to commit because it contains no credentials.

If `database.password` appears in the YAML file, startup fails. That includes an empty password.

Unknown YAML keys fail startup so typos do not silently fall back to defaults.

## Environment

| Variable | Required | Effect |
| --- | --- | --- |
| `WALLET_DB_PASSWORD` | Yes, unless `DATABASE_URL` is set | Database password. Never read from YAML. |
| `DATABASE_URL` | No | Full DSN. When set, host/port/user/password/name/sslmode from the file are not used for the connection. |
| `WALLET_DB_HOST` | No | Overrides `database.host` |
| `WALLET_DB_PORT` | No | Overrides `database.port` |
| `WALLET_DB_NAME` | No | Overrides `database.name` |
| `WALLET_DB_USER` | No | Overrides `database.user` |
| `WALLET_DB_SSLMODE` | No | Overrides `database.sslmode` |
| `WALLET_CONFIG` | No | Config file path |

Docker Compose sets `WALLET_DB_HOST=postgres` and `WALLET_DB_PASSWORD`. The published `config.yaml` keeps `host: localhost` for runs on the host machine.

## Timeouts

- `server.requestTimeout` bounds a single request context. The database call observes that context.
- `database.lockTimeout` and `database.statementTimeout` are applied with `SET LOCAL` inside each transfer transaction.
- `database.connectRetries` covers PostgreSQL still starting when the API process starts.
- `server.shutdownTimeout` bounds `http.Server.Shutdown` after `SIGINT` or `SIGTERM`.

## Logging

Startup logs the database host, name, and user. It does not log the password or a raw DSN.
