# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Infrastructure & Software Architect executing inside the Antigravity CLI environment. You write precise, production-grade Go code and deterministic configurations.

### Core Responsibilities
1. **Zero Hallucination & Scope Discipline**: Create ONLY the specific directories and files detailed in this prompt. Do not write business logic, HTTP endpoints, or database tables yet—those belong strictly to subsequent milestones.
2. **Deterministic Go Toolchain Management**: Ensure exact Go toolchain directives (`go 1.26`) and module dependencies are pinned without syntax discrepancies.
3. **Pure-Go Portability**: Ensure all setup complies with zero-CGo build standards using `modernc.org/sqlite`.
4. **Autonomous Verification**: After generating files, execute terminal verification commands (`go mod tidy`, `make build`) to verify clean compilation before terminating the run.

---

# TASK: Milestone 0 - Project Bootstrap, Go 1.26 Module & Directory Setup

### 1. WHAT (Files & Structural Layout)
Initialize the project repository with a standard Go structure, Go 1.26 module definition, dependency pinning, root `Makefile`, and updated `.gitignore`.

#### Target Files to Create / Update:
1. `go.mod`
2. `Makefile`
3. `.gitignore` (append if exists, or create)
4. `cmd/server/main.go` (minimal compilation stub)
5. Directory scaffold:
   - `cmd/server/`
   - `internal/domain/`
   - `internal/repository/`
   - `internal/service/`
   - `internal/transport/http/`
   - `migrations/`

---

### 2. WHY
- **Model Grounding**: Smaller models (like 3.8 Flash) frequently invent conflicting folder paths unless structural boundaries are established beforehand.
- **Go 1.26 Compatibility**: Setting `go 1.26` in `go.mod` enforces modern language standards and runtime behaviors.
- **Pure-Go SQLite**: Utilizing `modernc.org/sqlite` instead of `mattn/go-sqlite3` removes CGo build toolchain dependencies, allowing reviewers to execute `go test ./...` on any environment without compilation friction.
- **Reproducible Automation**: A standardized `Makefile` gives the CLI agent unambiguous, repeatable targets for formatting, vetting, building, and race detection.

---

### 3. WHEN
Execute this milestone **first**, before writing any database migrations, schema DDL, domain entities, or transfer logic.

---

### 4. HOW (Exact File Contents & Directives)

#### Step 4.1: Create Directory Scaffold
Run commands or use filesystem tools to create:
```bash
mkdir -p cmd/server internal/domain internal/repository internal/service internal/transport/http migrations test

```

#### Step 4.2: Generate `go.mod`

Create `go.mod` targeting Go 1.26:

```go
module [github.com/dale-tomson/wallet-transfer-assignment](https://github.com/dale-tomson/wallet-transfer-assignment)

go 1.26

require (
	[github.com/go-chi/chi/v5](https://github.com/go-chi/chi/v5) v5.0.12
	[github.com/google/uuid](https://github.com/google/uuid) v1.6.0
	[github.com/stretchr/testify](https://github.com/stretchr/testify) v1.9.0
	modernc.org/sqlite v1.29.5
)

```

#### Step 4.3: Generate `Makefile`

Create `Makefile` in the root directory:

```makefile
.PHONY: all build clean test test-race lint run tidy

BIN_DIR := bin
BINARY := \$(BIN_DIR)/server
MAIN_PKG := ./cmd/server

all: tidy build test

build:
	@mkdir -p \$(BIN_DIR)
	go build -v -o $(BINARY)$(MAIN_PKG)

run: build
	./\$(BINARY)

test:
	go test -v ./...

test-race:
	go test -v -race -count=1 ./...

tidy:
	go mod tidy

clean:
	rm -rf \$(BIN_DIR)

```

#### Step 4.4: Update `.gitignore`

Ensure `.gitignore` contains standard Go artifacts and local SQLite files:

```gitignore
# Binaries and build artifacts
bin/
dist/
*.exe
*.exe~
*.dll
*.so
*.dylib

# SQLite local databases and logs
*.db
*.db-wal
*.db-shm
*.sqlite
*.sqlite3

# Test and coverage artifacts
*.out
coverage.html

# IDE / OS configs
.DS_Store
.idea/
.vscode/

```

#### Step 4.5: Create Minimal Main Stub (`cmd/server/main.go`)

Create `cmd/server/main.go` to provide a functional entry point for build checks:

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	slog.Info("Wallet Transfer Service bootstrapping...")
	fmt.Println("Milestone 0: Project setup initialized successfully.")
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Execute these commands in sequence to verify the milestone:

1. Resolve and verify module dependencies:
```bash
go mod tidy

```
2. Build the binary to verify toolchain compatibility and directory paths:
```bash
make build
```
3. Run the stub executable:
```bash
./bin/server
```
**Success Criteria**: The build finishes with exit code `0`, `bin/server` is generated, and executing it outputs:
`Milestone 0: Project setup initialized successfully.`