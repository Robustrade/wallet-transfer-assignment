# Wallet Transfer Service

## Overview

A wallet-to-wallet transfer service built using Spring Boot and PostgreSQL.

The service guarantees:

* Idempotent request processing
* Safe concurrent execution
* Double-entry ledger consistency
* Atomic transaction processing
* Retry-safe behavior

---

## Tech Stack

* Java 17
* Spring Boot
* Spring Data JPA
* PostgreSQL
* Flyway
* MapStruct
* Spring Retry
* JUnit 5
* Mockito

---

## Prerequisites

* Java 17
* Maven
* PostgreSQL

---

## Database Setup

Create a PostgreSQL database:

```sql
CREATE DATABASE wallet_transfer_db;
```

Update the datasource configuration:

```yaml
spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/wallet_transfer_db
    username: postgres
    password: your_password
```

---

## Running the Application

```bash
mvn clean install
mvn spring-boot:run
```

---

## API

### Create Transfer

**POST** `/transfers`

Request:

```json
{
  "idempotencyKey": "transfer-123",
  "fromWalletId": "wallet-id-1",
  "toWalletId": "wallet-id-2",
  "amount": 100
}
```

Response:

```json
{
  "transferId": "transfer-id",
  "status": "PROCESSED",
  "amount": 100
}
```

---

## Running Tests

```bash
mvn test
```

---

## Documentation

Detailed design and architecture decisions are available in:

`DESIGN_DOC.md`
