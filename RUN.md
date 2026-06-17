# Running the Wallet Transfer Service

Follow the instructions below to configure, build, run, and test the wallet transfer application.

---

## 1. Prerequisites

Ensure you have the following installed on your system:
- **Java Development Kit (JDK) 21**
- **Apache Maven 3.8+**
- **PostgreSQL** (running locally on port `5432`)

---

## 2. Database Configuration

The application expects a PostgreSQL database configured with the details in `src/main/resources/application.properties`.

By default, it uses the following fallback values:
- **Database Name**: `walletdb`
- **Username**: `parth`
- **Password**: `Be$t12345`
- **Host**: `localhost`
- **Port**: `5432`

### Environment Variables
You can override these defaults by setting the following environment variables:
- `DB_URL`: The JDBC URL (e.g., `jdbc:postgresql://localhost:5432/walletdb`)
- `DB_USERNAME`: The database user (e.g., `my_custom_user`)
- `DB_PASSWORD`: The database password (e.g., `my_custom_password`)

For example, to run the application with environment variables:
```bash
DB_URL=jdbc:postgresql://localhost:5432/my_db DB_USERNAME=my_user DB_PASSWORD=my_pass mvn spring-boot:run
```

Before running, make sure the target database is created:
```sql
CREATE DATABASE walletdb;
```

---

## 3. Build & Test

To compile the application and run all automated tests (including user registration, login, and concurrent wallet transfers):
```bash
mvn clean compile test
```

---

## 4. Run the Application

Start the Spring Boot application locally:
```bash
mvn spring-boot:run
```
By default, the service will start on port **`8080`**.

---

## 5. API Usage Examples (curl)

Once the service is running, you can interact with it via the following `curl` commands:

### A. Register a New User
Registers a new user, hashes their password securely using BCrypt, and automatically creates a new wallet with a balance of `0`.
```bash
curl -X POST http://localhost:8080/user/register \
     -H "Content-Type: application/json" \
     -d '{
       "username": "johndoe",
       "email": "johndoe@example.com",
       "phoneno": "1345672890",
       "password": "securepassword123"
     }'
```

### B. User Login
Authenticates an existing user by checking the BCrypt-encrypted password.
```bash
curl -X POST http://localhost:8080/user/login \
     -H "Content-Type: application/json" \
     -d '{
       "username": "johndoe",
       "password": "securepassword123"
     }'
```

### C. Execute a Wallet Transfer
Performs a double-entry, thread-safe, and idempotent wallet-to-wallet transfer.
```bash
curl -X POST http://localhost:8080/transfers \
     -H "Content-Type: application/json" \
     -d '{
       "idempotencyKey": "unique-uuid-key-12345",
       "fromWalletId": "source-wallet-uuid",
       "toWalletId": "destination-wallet-uuid",
       "amount": 100.00
     }'
```
*(Replace `source-wallet-uuid` and `destination-wallet-uuid` with actual wallet IDs returned during user registration).*
