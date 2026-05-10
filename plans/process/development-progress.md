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

---

## MySQL Proxy Implementation (2026-05-10)

### Architecture
The MySQL proxy (`pkg/mysqlproxy`) implements MySQL wire protocol v10 directly (no external MySQL protocol library). It reuses the same HTTP forwarding architecture as PgSQL: `httpclient` → AES-256-GCM → HTTP API server. Protocol disambiguation is handled via the `X-Protocol: mysql` HTTP header.

### Components Added

#### 1. MySQL Protocol (`pkg/mysqlproxy/protocol.go`)
- Manual MySQL wire protocol implementation (no external dependency)
- Packet I/O: `ReadPacket`/`WritePacket` (3-byte LE length + 1-byte sequence)
- Handshake: `HandshakeV10.Encode()` + `ParseHandshakeResponse()` 
- Status packets: `BuildOKPacket`, `BuildERRPacket`, `BuildEOFPacket`
- Result set: `ColumnDef41`, `BuildResultSetRow` (text protocol, NULL→0xFB)
- Length-encoded types: `PutLenEncInt`, `PutLenEncString`
- Supported capabilities: CLIENT_LONG_PASSWORD | PROTOCOL_41 | SECURE_CONNECTION | PLUGIN_AUTH | PLUGIN_AUTH_LENENC_CLIENT_DATA | CONNECT_WITH_DB

#### 2. MySQL Proxy (`pkg/mysqlproxy/proxy.go`)
- `Proxy` struct identical to pgproxy: `New(listenAddr, serverURL, encKey)`
- `handleConnection`: Handshake→Session→Command Loop→Cleanup
  - Handshake flow: HandshakeV10 → HandshakeResponse41 → SessionRequest("mysql") → OK
  - Supported commands: COM_QUERY, COM_PING, COM_QUIT, COM_INIT_DB
  - Unsupported: COM_STMT_PREPARE/EXECUTE/CLOSE/RESET/FETCH → error response
- `buildResultSet`: col_count → ColumnDef×N → EOF → Row×N → EOF
- `commandTag`: MySQL-style OK info string generation

#### 3. MySQL Proxy Tests (`pkg/mysqlproxy/proxy_test.go`)
- 15 test functions covering: packet roundtrip, handshake encode/decode, OK/ERR/EOF packet building, column def encoding, result set row, length-encoded integers, command decoding, SQL command extraction

#### 4. X-Protocol Disambiguation
- `pkg/httpclient/client.go`: `SessionRequest(params, protocol)` adds `X-Protocol` header
- `pkg/handler/adapter/gin.go` + `fiber.go`: Read `X-Protocol`, default to "pg"
- `pkg/pgproxy/proxy.go`: Updated to `SessionRequest(..., "pg")`

#### 5. Dual Protocol Proxy (`cmd/proxy/main.go`)
- Starts PgSQL proxy and/or MySQL proxy based on `proxy_protocol` config ("pg"/"mysql"/"both")
- Independent goroutines, shared signal handling, graceful shutdown

#### 6. Docker Compose MySQL Environment
- `docker-compose.yml`: Added `mysql:8.0` service with healthcheck
- `docker-compose.test.yml`: Test-specific environment (MySQL+PostgreSQL+Server+Proxy, isolated ports)
- `config.test.json`: Test configuration with `proxy_protocol: "both"`
- Proxy port mapping: `3307:3306` (production), `23307:3306` (test)
- Server depends on both postgres and mysql healthchecks

**Test Environment Credentials:**

| Service | Container Port | Host Port | Credentials |
|---------|---------------|-----------|-------------|
| PostgreSQL | 5432 | 25432 | user=postgres / password=postgres / db=testdb |
| MySQL | 3306 | 23306 | user=root / password=root / db=testdb |
| Server | 8080 | 28080 | encryption_key=http-pg-test-key |
| PG Proxy | 6543 | 26543 | — |
| MySQL Proxy | 3306 | 23307 | — |

#### 7. Magefile Build System (`magefile.go`)
- 10 targets: `Build`, `Test`, `TestIntegration`, `DockerUp`, `DockerDown`, `DockerBuild`, `Lint`, `Release`, `Clean`, `All`, `CI`
- `All` (default): lint → test → build
- `CI`: full pipeline (clean → lint → test → build + optional docker build)
- Race detection with automatic CGO fallback
- Cross-platform release builds (linux/darwin/windows × amd64/arm64)

### Test Results (2026-05-10)
```
pkg/crypto:     5 passed
pkg/handler:    20 passed
pkg/httpclient: 5 passed
pkg/httphandler:7 passed
pkg/mysqlpool:  2 passed
pkg/mysqlproxy: 15 passed  ← NEW
pkg/pgparser:   9 passed
pkg/pgpool:     2 passed
pkg/pgproxy:    4 passed
Total:          69/69 passed
```

### Mage Pipeline Verification
```
mage clean all:  ✅ (clean → lint → test → build, all passed)
go build ./...:   ✅
```

---

## Logo Redesign (2026-05-11)

Replaced the generic database cylinder logo with a cartoon character design:

**Left: PgSQL Elephant** (#336791 blue)
- Round head with big floppy ears (front + back for depth)
- Curved trunk pointing toward center
- Big sparkly kawaii eye with white highlight
- Blush on cheek, small smile

**Right: MySQL Dolphin** (#F29111 orange)
- Round head with dorsal fin on top
- Pointed beak/snout pointing toward center
- Pectoral fin + tail fin for recognition
- Big sparkly kawaii eye with white highlight
- Blush on cheek, small smile

**Center: HTTP Proxy**
- Server rack box with blue status LEDs
- Left arrow (Elephant → Proxy) and right arrow (Proxy → Dolphin)
- Encryption lock icon above
- Blinking green activity light

**Bottom: Text**
- "HTTP-PG" in bold white (40px, tracking 6)
- "DATABASE PROXY" subtitle in muted blue (12px, tracking 4)

---

## CI/CD Pipeline (2026-05-11)

### GitHub Actions Workflows

#### 1. CI (`ci.yml`)
Triggered on push/PR to `main`, `develop` branches.

| Job | Platform | Dependencies | Description |
|-----|----------|-------------|-------------|
| `lint` | ubuntu-latest | — | `go vet ./...` |
| `unit-tests` | ubuntu/macos/windows | — | Unit tests on 3 OS matrix |
| `build` | ubuntu/macos/windows × gin/fiber | — | Build matrix: 3 OS × 2 frameworks |
| `integration-tests` | ubuntu-latest | lint + unit-tests + build | Docker-based real DB tests |
| `e2e-tests` | ubuntu-latest | lint + unit-tests + build | Full proxy pipeline tests |

Key features:
- Concurrency group with cancel-in-progress for redundant runs
- Docker healthcheck wait with curl polling (up to 120s)
- Docker logs dump on failure
- Always-run cleanup (`docker compose down --volumes`)

#### 2. Release (`release.yml`)
Triggered on tags matching `v*`.

- Cross-compiles 13 artifacts: server/proxy × linux/darwin/windows × amd64/arm64 + server_fiber_linux_amd64
- SHA-256 checksums generated for all artifacts
- GitHub Release created with auto-generated release notes

#### 3. Docker (`docker.yml`)
Triggered on push/PR to `main` when source code changes.

- Builds server and proxy Docker images
- Uses Docker Buildx with GitHub Actions cache
- Verifies images were built successfully
