# Wallet Transfer Service

Spring Boot service for wallet-to-wallet transfers with PostgreSQL persistence, idempotent transfer handling, and double-entry ledger recording.

## Tech Stack

- Java 17
- Spring Boot 3
- Spring JDBC
- PostgreSQL
- Maven

## Project Structure

```text
src/main/java/com/viditec/wallettransfer
|-- api
|   |-- controller
|   |-- request
|   `-- response
|-- config
|-- domain
|-- dto
|-- exception
|-- mapper
|-- repository
`-- service
```

## Endpoints

- `POST /wallets` creates a wallet with an initial balance
- `GET /wallets/{walletId}` fetches the current wallet balance
- `POST /transfers` creates an idempotent wallet transfer

### Create Wallet Payload

```json
{
  "walletId": "wallet_1",
  "initialBalance": 1000
}
```

### Create Transfer Payload

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

## PostgreSQL Configuration

The application reads these properties:

- `DB_URL`
- `DB_USERNAME`
- `DB_PASSWORD`

Default local values are configured in [src/main/resources/application.yml](/C:/Users/abhin/Documents/Projects/Viditec/wallet-transfer-assignment/src/main/resources/application.yml).

## Run PostgreSQL Locally

Use Docker Compose:

```bash
docker compose up -d
```

This starts PostgreSQL on `localhost:5432` with:

- database: `wallet_transfer`
- username: `postgres`
- password: `postgres`

## Run the Application

```bash
./mvnw spring-boot:run
```

On Windows:

```powershell
.\mvnw.cmd spring-boot:run
```

## Notes

- database schema is initialized from [src/main/resources/schema.sql](/C:/Users/abhin/Documents/Projects/Viditec/wallet-transfer-assignment/src/main/resources/schema.sql)
- transfer behavior and design decisions are documented in [docs/design-note.md](/C:/Users/abhin/Documents/Projects/Viditec/wallet-transfer-assignment/docs/design-note.md)
- tests were not run in this environment per instruction

## AI Usage

This project was developed with assistance from `OpenAI Codex`.

Tool used:

- `Codex`

How I generally use AI tools:

- to speed up scaffolding and repetitive setup work
- to compare implementation approaches and tradeoffs
- to improve code organization and documentation quality

How AI was used in this project:

- to help scaffold the Spring Boot project structure
- to refine the package organization into `api`, `exception`, `service`, `repository`, `domain`, and `config`
- to help shape the PostgreSQL-based wallet transfer implementation and documentation

Session disclosure:

- the implementation work for this repository was assisted through a Codex session
