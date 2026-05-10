# HTTP-PG

![HTTP-PG Logo](assets/logo.svg)

A dual-protocol database proxy that forwards PostgreSQL and MySQL wire protocol messages to an HTTP API server with AES-256-GCM encryption. The server decrypts messages, executes SQL on real database connections, and returns results through the encrypted HTTP channel.

```
Client ──TCP/wire protocol──> Proxy ──HTTPS/AES-GCM──> HTTP Server ──> PostgreSQL / MySQL
```

---

## Features

- **Dual Database Support** — PostgreSQL and MySQL proxies side by side
- **Custom MySQL Wire Protocol** — Self-implemented MySQL protocol (no external library dependency)
- **End-to-End Encryption** — AES-256-GCM authenticated encryption for all HTTP traffic
- **Connection Pooling** — Efficient session-based connection management via pgxpool (PG) and database/sql (MySQL)
- **Session Management** — UUID-based session tracking, each connection gets a dedicated backend connection
- **Dual HTTP Framework** — Choose Gin (default) or Fiber via build tags
- **Framework-Agnostic Core** — Handler logic is HTTP framework independent; Gin and Fiber are thin adapters
- **Extended Query Protocol** — Full Parse/Bind/Describe/Execute/Sync sequence support for PgSQL
- **Build System** — Mage-based (Go-native Make alternative)
- **Docker Support** — Production and testing Docker Compose configurations

---

## Architecture

```
┌──────────────┐     PgSQL      ┌──────────────┐                  ┌──────────────┐
│  psql / pgx  │───wire proto──>│  PgSQL Proxy  │────HTTPS────>   │              │
└──────────────┘                │  (:5432)     │   AES-256-GCM   │  HTTP Server │
                                └──────────────┘                  │  (:8080)     │───> PostgreSQL
┌──────────────┐     MySQL      ┌──────────────┐                  │              │
│  mysql CLI   │───wire proto──>│  MySQL Proxy  │────HTTPS────>   │  (Gin/Fiber) │───> MySQL
│  / client    │                │  (:3306)     │   AES-256-GCM   │              │
└──────────────┘                └──────────────┘                  └──────────────┘
```

Each client TCP connection creates a UUID session on the HTTP server, which acquires a dedicated database connection from the pool. SQL statements are forwarded as encrypted JSON payloads over HTTP.

---

## Quick Start

### Prerequisites

- Go 1.25+
- PostgreSQL 14+ and/or MySQL 8.0
- Docker (optional, for containerized deployment)

### Install

```bash
git clone https://github.com/VDHewei/http-pg.git
cd http-pg
go mod tidy
```

### Configure

Set the encryption key (or put it in config.json):

```bash
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

Edit `config.json`:

```json
{
    "server_addr": ":8080",
    "proxy_addr": ":5432",
    "mysql_proxy_addr": ":3306",
    "proxy_protocol": "both",
    "postgres_dsn": "postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable",
    "mysql_dsn": "root:root@tcp(localhost:3306)/testdb",
    "encryption_key": "",
    "max_connections": 20,
    "min_connections": 5
}
```

### Run

Start the HTTP API server:

```bash
go run ./cmd/server -config config.json
```

Start the TCP proxy:

```bash
go run ./cmd/proxy -config config.json
```

### Connect

```bash
# PostgreSQL
psql -h localhost -p 5432 -U postgres -d testdb

# MySQL
mysql -h 127.0.0.1 -P 3306 -u root -proot testdb
```

### Docker (Production)

```bash
docker compose up -d
```

---

## HTTP Framework Selection

The server supports two HTTP frameworks selected at compile time via Go build tags:

| Framework | Build Command | Default |
|-----------|--------------|---------|
| **Gin** | `go build ./cmd/server` | Yes |
| **Fiber** | `go build -tags fiber ./cmd/server` | No |

The `pkg/handler/` package contains all business logic with zero framework dependency. Framework-specific code lives in `pkg/handler/adapter/` as thin route registrars.

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Health check with pool stats |
| POST | `/api/v1/session` | Create a database session |
| POST | `/api/v1/query` | Execute a SQL query |
| DELETE | `/api/v1/session/:id` | Close a session |

---

## Configuration

| Parameter | Env Var | Default | Description |
|-----------|---------|---------|-------------|
| `server_addr` | — | `:8080` | HTTP server listen address |
| `proxy_addr` | — | `:5432` | PgSQL proxy listen address |
| `mysql_proxy_addr` | — | `:3306` | MySQL proxy listen address |
| `proxy_protocol` | — | `"pg"` | Proxy mode: `"pg"`, `"mysql"`, or `"both"` |
| `postgres_dsn` | `POSTGRES_DSN` | — | PostgreSQL connection string |
| `mysql_dsn` | `MYSQL_DSN` | — | MySQL connection string |
| `encryption_key` | `ENCRYPTION_KEY` | *required* | AES-256-GCM encryption key |
| `max_connections` | — | `20` | Max backend connections per pool |
| `min_connections` | — | `5` | Min backend connections per pool |
| `server_url` | — | *auto* | Full URL of the HTTP server (auto-built from `server_addr` if empty) |

---

## Project Structure

```
http-pg/
├── cmd/
│   ├── proxy/main.go              # TCP proxy entry (PG + MySQL)
│   └── server/main.go             # HTTP API server entry
├── pkg/
│   ├── crypto/                    # AES-256-GCM encrypt/decrypt + key derivation
│   ├── pgparser/                  # PgSQL wire protocol message parser
│   ├── pgpool/                    # PgSQL connection pool (pgxpool-based)
│   ├── pgproxy/                   # PgSQL TCP proxy (pgproto3-based)
│   ├── mysqlpool/                 # MySQL connection pool (database/sql-based)
│   ├── mysqlproxy/                # MySQL TCP proxy + custom wire protocol
│   ├── httpclient/                # HTTP client for proxy → server communication
│   ├── handler/                   # Framework-agnostic core handler
│   │   └── adapter/               # Gin / Fiber route registrars
│   └── httphandler/               # Legacy Gin-coupled handler (deprecated)
├── internal/config/               # Shared configuration
├── test/
│   ├── integration/               # Integration tests (requires databases)
│   └── e2e/                       # End-to-end tests (comprehensive SQL tests)
├── assets/                        # Logo assets (SVG + ASCII)
├── docker-compose.yml             # Production Docker Compose
├── docker-compose.test.yml        # Test Docker Compose
├── Dockerfile                     # Multi-stage build
├── magefile.go                    # Mage build system
├── config.json                    # Production config
├── config.test.json               # Testing config
└── go.mod
```

---

## Build System

The project uses [Mage](https://magefile.org/) instead of Makefile:

```bash
# Install Mage
go install github.com/magefile/mage@latest

# Available targets
mage build                  # Compile server and proxy binaries
mage test                   # Run all unit tests (pkg/...)
mage testintegration        # Run integration tests (requires Docker)
mage lint                   # go vet ./...
mage release                # Cross-compile for linux/darwin/windows × amd64/arm64
mage dockerUp               # docker compose up -d (test environment)
mage dockerDown             # docker compose down --volumes
mage dockerBuild            # docker compose build --no-cache
mage ci                     # Full CI pipeline: clean → lint → test → build → dockerBuild
mage all                    # Lint → test → build
mage clean                  # Clean build artifacts
```

---

## Testing

### Docker Test Environment

The project uses `docker-compose.test.yml` for testing, which starts these services:

| Service | Image | Port (host:container) | Credentials |
|---------|-------|-----------------------|-------------|
| **PostgreSQL** | postgres:14-alpine | `25432:5432` | user=`postgres` / password=`postgres` / db=`testdb` |
| **MySQL** | mysql:8.0 | `23306:3306` | user=`root` / password=`root` / db=`testdb` |
| **Server** | self-built | `28080:8080` | encryption key=`http-pg-test-key` |
| **Proxy** (PG+MySQL) | self-built | `26543:6543` (PG), `23307:3306` (MySQL) | encryption key=`http-pg-test-key` |

The test config file `config.test.json` connects to these ports automatically.

```bash
# Start test environment (use docker compose directly)
docker compose -f docker-compose.test.yml up -d

# Verify all services are healthy
curl http://localhost:28080/api/v1/health

# Stop and clean up (clears database volumes)
docker compose -f docker-compose.test.yml down --volumes
```

Or via mage:

```bash
mage dockerUp
mage dockerDown
```

### Unit Tests

```bash
go test ./pkg/... -v
```

### Integration Tests (requires Docker databases)

```bash
mage dockerUp
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
mage dockerDown
```

### End-to-End Tests (full proxy pipeline)

The E2E tests spin up their own server+proxy programmatically (they reuse the Docker PostgreSQL and MySQL databases):

```bash
# Run both PgSQL and MySQL E2E tests
mage dockerUp
go test -v ./test/e2e/ -timeout 120s
mage dockerDown

# Run only PgSQL tests
go test -v -run 'TestPgSQLComprehensive' ./test/e2e/ -timeout 120s

# Run only MySQL tests
go test -v -run 'TestMySQLComprehensive' ./test/e2e/ -timeout 120s
```

### Test Coverage

| Test Layer | Package | Tests | Requirements |
|------------|---------|-------|-------------|
| Unit | `pkg/crypto` | 5 | None |
| Unit | `pkg/pgparser` | 9 | None |
| Unit | `pkg/pgpool` | 2 | Docker PostgreSQL |
| Unit | `pkg/httpclient` | 5 | None |
| Unit | `pkg/httphandler` | 7 | None |
| Unit | `pkg/handler` | 20 | None |
| Unit | `pkg/pgproxy` | 4 | None |
| Unit | `pkg/mysqlpool` | 2 | Docker MySQL |
| Unit | `pkg/mysqlproxy` | 15 | None |
| Integration | `test/integration` | 2 | `mage dockerUp` + `-tags integration` |
| E2E — PgSQL | `test/e2e` | 9 subtests | `mage dockerUp` |
| E2E — MySQL | `test/e2e` | 9 subtests | `mage dockerUp` |

All 69 unit tests pass:

```
pkg/crypto:     5 passed
pkg/handler:    20 passed
pkg/httpclient: 5 passed
pkg/httphandler:7 passed
pkg/mysqlpool:  2 passed
pkg/mysqlproxy: 15 passed
pkg/pgparser:   9 passed
pkg/pgpool:     2 passed
pkg/pgproxy:    4 passed
Total:          69/69 passed
```

---

## Wire Protocol Details

### PgSQL (via pgx/pgproto3)

- Full PostgreSQL wire protocol v3 support
- Simple query (`'Q'`) and extended query protocol (`Parse`/`Bind`/`Describe`/`Execute`/`Sync`)
- RowDescription with text-format columns, DataRow with raw values
- PgSQL proxy uses `github.com/jackc/pgx/v5/pgproto3` for message encoding/decoding

### MySQL (self-implemented)

- Custom implementation of MySQL text protocol (no external wire protocol library)
- HandshakeV10, HandshakeResponse41, COM_QUERY, COM_PING, COM_QUIT, COM_INIT_DB
- Text protocol result sets: column count → ColumnDef×N → EOF → row×N → EOF
- Length-encoded integer and string encoding per MySQL spec
- All state-management commands (COM_STMT_PREPARE/EXECUTE/CLOSE) return errors

---

## Dependencies

- [pgx v5](https://github.com/jackc/pgx) — PostgreSQL driver, pgproto3, pgxpool
- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) — MySQL driver
- [Gin](https://github.com/gin-gonic/gin) — HTTP web framework (default)
- [Fiber v3](https://github.com/gofiber/fiber) — HTTP web framework (optional, via `-tags fiber`)
- [google/uuid](https://github.com/google/uuid) — UUID generation

---

## License

MIT
