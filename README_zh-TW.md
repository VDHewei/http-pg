# HTTP-PG

![HTTP-PG Logo](assets/logo.svg)

一款雙協議資料庫代理，將 PostgreSQL 和 MySQL 的有線協定訊息經由 HTTP API 伺服器轉發，使用 AES-256-GCM 加密。伺服器解密訊息後，在真實資料庫連線上執行 SQL，並透過加密的 HTTP 通道回傳結果。

```
客戶端 ──TCP/有線協定──> 代理 ──HTTPS/AES-GCM──> HTTP 伺服器 ──> PostgreSQL / MySQL
```

---

## 特性

- **雙資料庫支援** — PostgreSQL 和 MySQL 代理並行執行
- **自研 MySQL 有線協定** — 無外部協定庫依賴
- **端到端加密** — 所有 HTTP 流量使用 AES-256-GCM 認證加密
- **連線池管理** — 基於 pgxpool（PG）和 database/sql（MySQL）的會話級連線管理
- **會話管理** — 每個客戶端連線使用 UUID 追蹤，獲取專用後端連線
- **雙 HTTP 框架** — 預設 Gin，可透過 build tag 切換到 Fiber
- **框架無關核心** — 業務邏輯不依賴任何 HTTP 框架；Gin 和 Fiber 是薄適配層
- **擴展查詢協定** — 完整支援 PgSQL 的 Parse/Bind/Describe/Execute/Sync 序列
- **建構系統** — 基於 Mage（Go 原生的 Make 替代品）
- **Docker 支援** — 生產和測試兩套 Docker Compose 配置

---

## 架構

```
┌──────────────┐    PgSQL      ┌──────────────┐                  ┌──────────────┐
│  psql / pgx  │──有線協定───>  │  PgSQL 代理   │────HTTPS───>   │              │
└──────────────┘               │  (:5432)     │  AES-256-GCM   │  HTTP 伺服器  │───> PostgreSQL
                               └──────────────┘                  │  (:8080)     │
┌──────────────┐    MySQL      ┌──────────────┐                  │  (Gin/Fiber) │───> MySQL
│  mysql 客戶端 │──有線協定───>  │  MySQL 代理   │────HTTPS───>   │              │
└──────────────┘               │  (:3306)     │  AES-256-GCM   └──────────────┘
                               └──────────────┘
```

每個客戶端 TCP 連線在 HTTP 伺服器上建立一個 UUID 會話，並從連線池中獲取一個專用資料庫連線。SQL 語句以加密的 JSON 酬載經由 HTTP 轉發。

---

## 快速開始

### 前置條件

- Go 1.25+
- PostgreSQL 14+ 和/或 MySQL 8.0
- Docker（可選，用於容器化部署）

### 安裝

```bash
git clone https://github.com/VDHewei/http-pg.git
cd http-pg
go mod tidy
```

### 配置

設定加密金鑰（或寫入 config.json）：

```bash
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

編輯 `config.json`：

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

### 執行

啟動 HTTP API 伺服器：

```bash
go run ./cmd/server -config config.json
```

啟動 TCP 代理：

```bash
go run ./cmd/proxy -config config.json
```

### 連線

```bash
# PostgreSQL
psql -h localhost -p 5432 -U postgres -d testdb

# MySQL
mysql -h 127.0.0.1 -P 3306 -u root -proot testdb
```

### Docker（生產環境）

```bash
docker compose up -d
```

---

## HTTP 框架選擇

伺服器在編譯時透過 Go build tags 選擇框架：

| 框架 | 建構命令 | 預設 |
|------|---------|------|
| **Gin** | `go build ./cmd/server` | 是 |
| **Fiber** | `go build -tags fiber ./cmd/server` | 否 |

`pkg/handler/` 包含所有業務邏輯，零框架依賴。框架特定程式碼位於 `pkg/handler/adapter/`，是薄的路由註冊層。

---

## API 端點

| 方法 | 路徑 | 描述 |
|------|------|------|
| GET | `/api/v1/health` | 健康檢查及連線池狀態 |
| POST | `/api/v1/session` | 建立資料庫會話 |
| POST | `/api/v1/query` | 執行 SQL 查詢 |
| DELETE | `/api/v1/session/:id` | 關閉會話 |

---

## 配置參數

| 參數 | 環境變數 | 預設值 | 描述 |
|------|---------|--------|------|
| `server_addr` | — | `:8080` | HTTP 伺服器監聽位址 |
| `proxy_addr` | — | `:5432` | PgSQL 代理監聽位址 |
| `mysql_proxy_addr` | — | `:3306` | MySQL 代理監聽位址 |
| `proxy_protocol` | — | `"pg"` | 代理模式：`"pg"` / `"mysql"` / `"both"` |
| `postgres_dsn` | `POSTGRES_DSN` | — | PostgreSQL 連線字串 |
| `mysql_dsn` | `MYSQL_DSN` | — | MySQL 連線字串 |
| `encryption_key` | `ENCRYPTION_KEY` | *必填* | AES-256-GCM 加密金鑰 |
| `max_connections` | — | `20` | 每個連線池的最大連線數 |
| `min_connections` | — | `5` | 每個連線池的最小連線數 |
| `server_url` | — | *自動* | HTTP 伺服器完整 URL（為空時從 server_addr 自動拼接） |

---

## 專案結構

```
http-pg/
├── cmd/
│   ├── proxy/main.go              # TCP 代理入口（PG + MySQL）
│   └── server/main.go             # HTTP API 伺服器入口
├── pkg/
│   ├── crypto/                    # AES-256-GCM 加解密 + 金鑰推導
│   ├── pgparser/                  # PgSQL 有線協定訊息解析器
│   ├── pgpool/                    # PgSQL 連線池（基於 pgxpool）
│   ├── pgproxy/                   # PgSQL TCP 代理（基於 pgproto3）
│   ├── mysqlpool/                 # MySQL 連線池（基於 database/sql）
│   ├── mysqlproxy/                # MySQL TCP 代理 + 自研有線協定
│   ├── httpclient/                # 代理到伺服器的 HTTP 用戶端
│   ├── handler/                   # 框架無關的核心處理器
│   │   └── adapter/               # Gin / Fiber 路由註冊器
│   └── httphandler/               # 舊版 Gin 耦合處理器（已棄用）
├── internal/config/               # 共享配置
├── test/
│   ├── integration/               # 整合測試（需資料庫）
│   └── e2e/                       # 端到端測試（全面 SQL 測試）
├── assets/                        # Logo 資產（SVG + ASCII）
├── docker-compose.yml             # 生產 Docker Compose
├── docker-compose.test.yml        # 測試 Docker Compose
├── Dockerfile                     # 多階段建構
├── magefile.go                    # Mage 建構系統
├── config.json                    # 生產配置
├── config.test.json               # 測試配置
└── go.mod
```

---

## 建構系統

專案使用 [Mage](https://magefile.org/) 替代 Makefile：

```bash
# 安裝 Mage
go install github.com/magefile/mage@latest

# 可用命令
mage build                  # 編譯 server 和 proxy 二進位
mage test                   # 執行所有單元測試
mage testintegration        # 執行整合測試（需 Docker）
mage lint                   # go vet ./...
mage release                # 交叉編譯 linux/darwin/windows × amd64/arm64
mage dockerUp               # docker compose up -d（測試環境）
mage dockerDown             # docker compose down --volumes
mage dockerBuild            # docker compose build --no-cache
mage ci                     # 完整 CI 流水線
mage all                    # Lint → test → build
mage clean                  # 清理建構產物
```

---

## 測試

### Docker 測試環境

使用 `docker-compose.test.yml` 啟動測試環境，包含以下服務：

| 服務 | 映像 | 埠映射 (主機:容器) | 帳號密碼 |
|------|------|-------------------|----------|
| **PostgreSQL** | postgres:14-alpine | `25432:5432` | 使用者=`postgres` / 密碼=`postgres` / 資料庫=`testdb` |
| **MySQL** | mysql:8.0 | `23306:3306` | 使用者=`root` / 密碼=`root` / 資料庫=`testdb` |
| **Server** | 自建 | `28080:8080` | 加密金鑰=`http-pg-test-key` |
| **Proxy** (PG+MySQL) | 自建 | `26543:6543` (PG), `23307:3306` (MySQL) | 加密金鑰=`http-pg-test-key` |

測試設定檔 `config.test.json` 自動連接到以上埠。

```bash
# 啟動測試環境（直接使用 docker compose）
docker compose -f docker-compose.test.yml up -d

# 驗證所有服務健康
curl http://localhost:28080/api/v1/health

# 停止並清理（清除資料庫資料）
docker compose -f docker-compose.test.yml down --volumes
```

或透過 mage 命令：

```bash
mage dockerUp
mage dockerDown
```

### 單元測試

```bash
go test ./pkg/... -v
```

### 整合測試（需要 Docker 資料庫）

```bash
mage dockerUp
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
mage dockerDown
```

### 端到端測試（完整代理鏈路）

E2E 測試會自動啟動 server+proxy（複用 Docker PostgreSQL 和 MySQL 資料庫）：

```bash
# 同時執行 PgSQL 和 MySQL E2E 測試
mage dockerUp
go test -v ./test/e2e/ -timeout 120s
mage dockerDown

# 僅執行 PgSQL 測試
go test -v -run 'TestPgSQLComprehensive' ./test/e2e/ -timeout 120s

# 僅執行 MySQL 測試
go test -v -run 'TestMySQLComprehensive' ./test/e2e/ -timeout 120s
```

### 測試覆蓋

| 層級 | 套件 | 測試數 | 依賴 |
|------|------|--------|------|
| 單元 | `pkg/crypto` | 5 | 無 |
| 單元 | `pkg/pgparser` | 9 | 無 |
| 單元 | `pkg/pgpool` | 2 | Docker PostgreSQL |
| 單元 | `pkg/httpclient` | 5 | 無 |
| 單元 | `pkg/httphandler` | 7 | 無 |
| 單元 | `pkg/handler` | 20 | 無 |
| 單元 | `pkg/pgproxy` | 4 | 無 |
| 單元 | `pkg/mysqlpool` | 2 | Docker MySQL |
| 單元 | `pkg/mysqlproxy` | 15 | 無 |
| 整合 | `test/integration` | 2 | `mage dockerUp` + `-tags integration` |
| E2E — PgSQL | `test/e2e` | 9 個子測試 | `mage dockerUp` |
| E2E — MySQL | `test/e2e` | 9 個子測試 | `mage dockerUp` |

全部 69 個單元測試通過：

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

## 有線協定實現細節

### PgSQL（基於 pgx/pgproto3）

- 完整支援 PostgreSQL wire protocol v3
- 簡單查詢（`'Q'`）和擴展查詢協定（Parse/Bind/Describe/Execute/Sync）
- RowDescription 使用文字格式，DataRow 包含原始值
- 使用 `github.com/jackc/pgx/v5/pgproto3` 編解碼訊息

### MySQL（自研實現）

- 無外部協定庫依賴，純手寫實現
- HandshakeV10、HandshakeResponse41、COM_QUERY、COM_PING、COM_QUIT、COM_INIT_DB
- 文字協定結果集：列計數 → ColumnDef×N → EOF → 資料行×N → EOF
- 按 MySQL 規範實現長度編碼整數和字串編碼
- 所有狀態管理命令（COM_STMT_PREPARE/EXECUTE/CLOSE）回傳錯誤

---

## 依賴庫

- [pgx v5](https://github.com/jackc/pgx) — PostgreSQL 驅動、pgproto3、pgxpool
- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) — MySQL 驅動
- [Gin](https://github.com/gin-gonic/gin) — HTTP Web 框架（預設）
- [Fiber v3](https://github.com/gofiber/fiber) — HTTP Web 框架（可選，透過 `-tags fiber`）
- [google/uuid](https://github.com/google/uuid) — UUID 生成

---

## 授權

MIT
