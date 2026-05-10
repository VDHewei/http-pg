# HTTP-PG: PgSQL Proxy over HTTP with Encryption

![HTTP-PG Logo](logo.svg)

A PostgreSQL proxy that forwards PgSQL wire protocol messages to an HTTP API server with AES-256-GCM encryption. The server decrypts the messages, executes SQL on real PostgreSQL connections, and returns results through the encrypted HTTP channel.

## Architecture

```
PgSQL Client  --[TCP/wire protocol]-->  Proxy  --[HTTPS/AES-GCM]-->  HTTP Server  -->  PostgreSQL
```

## Features

- **PgSQL Wire Protocol**: Full PostgreSQL wire protocol v3 support via `pgx/pgproto3`
- **End-to-End Encryption**: AES-256-GCM authenticated encryption for all HTTP traffic
- **Connection Pooling**: Efficient PostgreSQL connection management via `pgxpool`
- **Session Management**: UUID-based session tracking per client connection
- **SQL Parsing**: Extracts and logs SQL statements from wire protocol messages
- **Extensible**: Clean separation of proxy, client, handler, and pool layers

## Quick Start

### Prerequisites

- Go 1.25+
- PostgreSQL 14+

### Install

```bash
git clone https://github.com/http-pg/http-pg.git
cd http-pg
go mod tidy
```

### Configure

```bash
# Generate encryption key
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

Edit `config.json`:
```json
{
    "server_addr": ":8080",
    "proxy_addr": ":5432",
    "postgres_dsn": "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
    "max_connections": 20,
    "min_connections": 5
}
```

### Run

Start the HTTP API server:
```bash
go run ./cmd/server -config config.json
```

Start the PgSQL proxy:
```bash
go run ./cmd/proxy -config config.json
```

Connect with any PgSQL client:
```bash
psql -h localhost -p 5432 -U postgres
```

### Docker

```bash
docker-compose up -d
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Health check with pool stats |
| POST | `/api/v1/session` | Create a new PgSQL session |
| POST | `/api/v1/query` | Execute a SQL query |
| DELETE | `/api/v1/session/:id` | Close a session |

## Configuration

| Parameter | Env Var | Default | Description |
|-----------|---------|---------|-------------|
| `server_addr` | - | `:8080` | HTTP server listen address |
| `proxy_addr` | - | `:5432` | PgSQL proxy listen address |
| `postgres_dsn` | `POSTGRES_DSN` | `localhost:5432` | PostgreSQL connection string |
| `encryption_key` | `ENCRYPTION_KEY` | *required* | AES-256-GCM encryption key |
| `max_connections` | - | `20` | Max PG connections |
| `min_connections` | - | `5` | Min PG connections |

## Project Structure

```
http-pg/
├── cmd/
│   ├── proxy/main.go          # PgSQL proxy entry point
│   └── server/main.go         # HTTP API server entry point
├── pkg/
│   ├── crypto/                # AES-256-GCM encrypt/decrypt
│   ├── pgparser/              # PgSQL wire protocol parser
│   ├── pgpool/                # PgSQL connection pool manager
│   ├── pgproxy/               # PgSQL TCP proxy
│   ├── httpclient/            # HTTP client for proxy
│   └── httphandler/           # Gin HTTP API handlers
├── internal/config/           # Shared configuration
├── test/integration/          # Integration tests
├── docker-compose.yml         # Docker deployment
└── config.json                # Configuration file
```

## Testing

```bash
# Unit tests
go test ./pkg/... -v

# Integration tests (requires PostgreSQL)
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
```

## Dependencies

- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver and toolkit
- [Gin](https://github.com/gin-gonic/gin) - HTTP web framework
- [UUID](https://github.com/google/uuid) - UUID generation

## License

MIT
