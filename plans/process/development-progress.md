# Development Progress

## Architecture
The project implements a PgSQL proxy that forwards PostgreSQL wire protocol messages through an HTTP API server with AES-256-GCM encryption.

## Completed Components

### 1. Cryptography (`pkg/crypto`)
- AES-256-GCM encryption/decryption
- Key derivation via SHA-256
- Random key generation
- 5 unit tests covering roundtrip, wrong key, empty plaintext, key validation

### 2. PgSQL Parser (`pkg/pgparser`)
- Message encoding/decoding for wire transport
- Startup message parsing
- SQL extraction from Query and Parse messages
- Statement name extraction from Parse messages
- Message type identification
- 9 unit tests covering all parsing operations

### 3. Connection Pool (`pkg/pgpool`)
- PostgreSQL connection pool via pgxpool
- Session-based connection management
- SQL execution (ExecSQL for queries, ExecCommand for DML/DDL)
- Pool statistics
- 2 unit tests

### 4. HTTP Client (`pkg/httpclient`)
- POST-based message forwarding
- AES-256-GCM encryption of all requests
- Session creation and termination
- 5 unit tests with mock HTTP server

### 5. HTTP Handler (`pkg/httphandler`)
- Gin-based REST API
- Session management with cleanup
- Query execution with command-type routing (SELECT vs DML/DDL)
- Extended query protocol support (Parse/Execute with statement caching)
- Encrypted request/response
- Health check with pool stats
- Pool interface for testability
- 7 unit tests

### 6. PgSQL Proxy (`pkg/pgproxy`)
- TCP listener accepting PgSQL client connections
- **FIXED**: Session creation now properly forwarded to HTTP server via SessionRequest
- Startup message handling with authentication response
- Message forwarding to HTTP server
- Response construction (RowDescription, DataRow, CommandComplete)
- **FIXED**: CommandComplete tag reflects actual command type (SELECT/INSERT/UPDATE/DELETE/CREATE etc.)
- Error handling
- Graceful shutdown via quit channel

### 7. Entry Points
- `cmd/server`: HTTP API server with Gin
- `cmd/proxy`: PgSQL TCP proxy
- **FIXED**: Graceful shutdown - server now calls srv.Shutdown() with timeout

### 8. Integration Tests
- Docker-compose setup for PostgreSQL
- End-to-end test flow
- Build-tagged for optional execution

### 9. Documentation
- English README
- Simplified Chinese README (zh)
- Traditional Chinese README (zh-TW)
- **UPDATED**: Logo redesigned as SVG, used in all READMEs replacing ASCII art
- Architecture documentation

## Test Results
```
pkg/crypto:     5 passed
pkg/pgparser:   9 passed
pkg/pgpool:     2 passed
pkg/httpclient: 5 passed
pkg/httphandler:7 passed
Total:         25/25 passed
```

## Build Status
- `go build ./...` - SUCCESS
- `go build ./cmd/proxy/` - SUCCESS
- `go build ./cmd/server/` - SUCCESS
- `go build -tags integration ./test/...` - SUCCESS

## Issues Fixed (May 2026)
1. **Session creation flow**: Proxy now calls SessionRequest on HTTP server before queries
2. **CommandComplete tag**: Now reflects actual SQL command type instead of always "SELECT"
3. **DML/DDL routing**: Handler routes INSERT/UPDATE/DELETE/CREATE/etc to ExecCommand, SELECT to ExecSQL
4. **Extended query protocol**: Handler caches Parse'd statements and executes on Bind/Execute
5. **Graceful shutdown**: Server entry point uses srv.Shutdown() with timeout; Proxy uses quit channel

## Remaining Work
- Add unit tests for pkg/pgproxy
- Full Extended Query Protocol with parameter binding support (currently supports unnamed statements only)
- Config validation
- pgpool real database integration tests
