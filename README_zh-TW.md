# HTTP-PG: PgSQL 代理透過 HTTP 加密介面轉發

```
  __    __  ________  ________  _____       ________  ________
 / /   / / /_  __/ / /_  __/ / / ___ \     / ___/ _ \/ ___/ _ \
/ /_  / /   / / / /   / /   / / /_/ /    / (_ /  __/ (_ / , _/
\___/ /_/   /_/ /_/   /_/   /_/\____/     \___/\___/\___/_/|_/

      HTTP-PG  PgSQL 代理 · HTTP 加密傳輸
```

一個將 PgSQL 有線協定訊息透過 HTTP API 伺服器轉發的 PostgreSQL 代理，使用 AES-256-GCM 加密。伺服器解密訊息後，在真實的 PostgreSQL 連線上執行 SQL，並透過加密的 HTTP 通道回傳結果。

## 架構

```
PgSQL 用戶端  --[TCP/有線協定]-->  代理  --[HTTPS/AES-GCM]-->  HTTP 伺服器  -->  PostgreSQL
```

## 特性

- **PgSQL 有線協定**: 透過 `pgx/pgproto3` 完整支援 PostgreSQL 有線協定 v3
- **端對端加密**: 所有 HTTP 流量使用 AES-256-GCM 認證加密
- **連線池管理**: 透過 `pgxpool` 實現高效的 PostgreSQL 連線管理
- **工作階段管理**: 每個用戶端連線使用 UUID 進行工作階段追蹤
- **SQL 解析**: 從有線協定訊息中擷取並記錄 SQL 陳述式
- **可擴充架構**: 代理、用戶端、處理器、連線池各層清晰分離

## 快速開始

### 前置條件

- Go 1.25+
- PostgreSQL 14+

### 安裝

```bash
git clone https://github.com/http-pg/http-pg.git
cd http-pg
go mod tidy
```

### 配置

```bash
# 生成加密金鑰
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

編輯 `config.json`:
```json
{
    "server_addr": ":8080",
    "proxy_addr": ":5432",
    "postgres_dsn": "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
    "max_connections": 20,
    "min_connections": 5
}
```

### 執行

啟動 HTTP API 伺服器:
```bash
go run ./cmd/server -config config.json
```

啟動 PgSQL 代理:
```bash
go run ./cmd/proxy -config config.json
```

使用任意 PgSQL 用戶端連接:
```bash
psql -h localhost -p 5432 -U postgres
```

### Docker

```bash
docker-compose up -d
```

## API 端點

| 方法 | 路徑 | 描述 |
|--------|------|-------------|
| GET | `/api/v1/health` | 健康檢查及連線池狀態 |
| POST | `/api/v1/session` | 建立新的 PgSQL 工作階段 |
| POST | `/api/v1/query` | 執行 SQL 查詢 |
| DELETE | `/api/v1/session/:id` | 關閉工作階段 |

## 配置參數

| 參數 | 環境變數 | 預設值 | 描述 |
|-----------|---------|---------|-------------|
| `server_addr` | - | `:8080` | HTTP 伺服器監聽位址 |
| `proxy_addr` | - | `:5432` | PgSQL 代理監聽位址 |
| `postgres_dsn` | `POSTGRES_DSN` | `localhost:5432` | PostgreSQL 連線字串 |
| `encryption_key` | `ENCRYPTION_KEY` | *必填* | AES-256-GCM 加密金鑰 |
| `max_connections` | - | `20` | 最大 PG 連線數 |
| `min_connections` | - | `5` | 最小 PG 連線數 |

## 專案結構

```
http-pg/
├── cmd/
│   ├── proxy/main.go          # PgSQL 代理入口
│   └── server/main.go         # HTTP API 伺服器入口
├── pkg/
│   ├── crypto/                # AES-256-GCM 加密/解密
│   ├── pgparser/              # PgSQL 有線協定解析器
│   ├── pgpool/                # PgSQL 連線池管理器
│   ├── pgproxy/               # PgSQL TCP 代理
│   ├── httpclient/            # 代理端 HTTP 用戶端
│   └── httphandler/           # Gin HTTP API 處理器
├── internal/config/           # 共享配置
├── test/integration/          # 整合測試
├── docker-compose.yml         # Docker 部署
└── config.json                # 設定檔
```

## 測試

```bash
# 單元測試
go test ./pkg/... -v

# 整合測試（需要 PostgreSQL）
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
```

## 依賴函式庫

- [pgx](https://github.com/jackc/pgx) - PostgreSQL 驅動及工具包
- [Gin](https://github.com/gin-gonic/gin) - HTTP Web 框架
- [UUID](https://github.com/google/uuid) - UUID 生成

## 授權條款

MIT
