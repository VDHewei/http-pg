# PgSQL HTTP Proxy - Task Breakdown

## Architecture
See [architecture.md](../../docs/architecture.md) for the design document.

## Task List

### Phase 1: Foundation
- [x] Task 1: Init project structure, go mod, config
- [x] Task 2: AES-256-GCM crypto layer
- [x] Task 3: PgSQL message parser (pgproto3 ↔ SQL)

### Phase 2: Core
- [x] Task 4: PgSQL connection pool (pgxpool)
- [x] Task 5: HTTP client for message forwarding
- [x] Task 6: HTTP API handler (Gin)

### Phase 3: Integration
- [x] Task 7: PgSQL TCP proxy
- [x] Task 8: Server entry point
- [x] Task 9: Integration tests

### Phase 4: Polish
- [x] Task 10: Project logo
- [x] Task 11: README (zh, en, zh-TW)
- [x] Task 12: Git init & commit

## Progress Tracking
See `plans/process/` for detailed progress notes per task.

## How to Run

### Start the HTTP server
```bash
go run ./cmd/server -config config.json
```

### Start the PgSQL proxy
```bash
go run ./cmd/proxy -config config.json
```

### Connect with psql
```bash
psql -h localhost -p 5432 -U postgres
```
