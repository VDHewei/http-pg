# HTTP-PG

![HTTP-PG Logo](assets/logo.svg)

一款双协议数据库代理，将 PostgreSQL 和 MySQL 的有线协议消息通过 HTTP API 服务器转发，使用 AES-256-GCM 加密。服务器解密消息后，在真实数据库连接上执行 SQL，并通过加密的 HTTP 通道返回结果。

```
客户端 ──TCP/有线协议──> 代理 ──HTTPS/AES-GCM──> HTTP 服务器 ──> PostgreSQL / MySQL
```

---

## 特性

- **双数据库支持** — PostgreSQL 和 MySQL 代理并排运行
- **自研 MySQL 有线协议** — 无外部协议库依赖
- **端到端加密** — 所有 HTTP 流量使用 AES-256-GCM 认证加密
- **连接池管理** — 基于 pgxpool（PG）和 database/sql（MySQL）的会话级连接管理
- **会话管理** — 每个客户端连接使用 UUID 追踪，获取专用后端连接
- **双 HTTP 框架** — 默认 Gin，可通过 build tag 切换到 Fiber
- **框架无关核心** — 业务逻辑不依赖任何 HTTP 框架；Gin 和 Fiber 是薄适配层
- **扩展查询协议** — 完整支持 PgSQL 的 Parse/Bind/Describe/Execute/Sync 序列
- **构建系统** — 基于 Mage（Go 原生的 Make 替代品）
- **Docker 支持** — 生产和测试两套 Docker Compose 配置

---

## 架构

```
┌──────────────┐    PgSQL      ┌──────────────┐                  ┌──────────────┐
│  psql / pgx  │──有线协议───>  │  PgSQL 代理   │────HTTPS───>   │              │
└──────────────┘               │  (:5432)     │  AES-256-GCM   │  HTTP 服务器  │───> PostgreSQL
                               └──────────────┘                  │  (:8080)     │
┌──────────────┐    MySQL      ┌──────────────┐                  │  (Gin/Fiber) │───> MySQL
│  mysql 客户端 │──有线协议───>  │  MySQL 代理   │────HTTPS───>   │              │
└──────────────┘               │  (:3306)     │  AES-256-GCM   └──────────────┘
                               └──────────────┘
```

每个客户端 TCP 连接在 HTTP 服务器上创建一个 UUID 会话，并从连接池中获取一个专用数据库连接。SQL 语句以加密的 JSON 载荷通过 HTTP 转发。

---

## 快速开始

### 前置条件

- Go 1.25+
- PostgreSQL 14+ 和/或 MySQL 8.0
- Docker（可选，用于容器化部署）

### 安装

```bash
git clone https://github.com/VDHewei/http-pg.git
cd http-pg
go mod tidy
```

### 配置

设置加密密钥（或写入 config.json）：

```bash
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

编辑 `config.json`：

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

### 运行

启动 HTTP API 服务器：

```bash
go run ./cmd/server -config config.json
```

启动 TCP 代理：

```bash
go run ./cmd/proxy -config config.json
```

### 连接

```bash
# PostgreSQL
psql -h localhost -p 5432 -U postgres -d testdb

# MySQL
mysql -h 127.0.0.1 -P 3306 -u root -proot testdb
```

### Docker（生产环境）

```bash
docker compose up -d
```

---

## HTTP 框架选择

服务器在编译时通过 Go build tags 选择框架：

| 框架 | 构建命令 | 默认 |
|------|---------|------|
| **Gin** | `go build ./cmd/server` | 是 |
| **Fiber** | `go build -tags fiber ./cmd/server` | 否 |

`pkg/handler/` 包含所有业务逻辑，零框架依赖。框架特定代码位于 `pkg/handler/adapter/`，是薄的路由注册层。

---

## API 端点

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/api/v1/health` | 健康检查及连接池状态 |
| POST | `/api/v1/session` | 创建数据库会话 |
| POST | `/api/v1/query` | 执行 SQL 查询 |
| DELETE | `/api/v1/session/:id` | 关闭会话 |

---

## 配置参数

| 参数 | 环境变量 | 默认值 | 描述 |
|------|---------|--------|------|
| `server_addr` | — | `:8080` | HTTP 服务器监听地址 |
| `proxy_addr` | — | `:5432` | PgSQL 代理监听地址 |
| `mysql_proxy_addr` | — | `:3306` | MySQL 代理监听地址 |
| `proxy_protocol` | — | `"pg"` | 代理模式：`"pg"` / `"mysql"` / `"both"` |
| `postgres_dsn` | `POSTGRES_DSN` | — | PostgreSQL 连接字符串 |
| `mysql_dsn` | `MYSQL_DSN` | — | MySQL 连接字符串 |
| `encryption_key` | `ENCRYPTION_KEY` | *必填* | AES-256-GCM 加密密钥 |
| `max_connections` | — | `20` | 每个连接池的最大连接数 |
| `min_connections` | — | `5` | 每个连接池的最小连接数 |
| `server_url` | — | *自动* | HTTP 服务器完整 URL（为空时从 server_addr 自动拼接） |

---

## 项目结构

```
http-pg/
├── cmd/
│   ├── proxy/main.go              # TCP 代理入口（PG + MySQL）
│   └── server/main.go             # HTTP API 服务器入口
├── pkg/
│   ├── crypto/                    # AES-256-GCM 加解密 + 密钥派生
│   ├── pgparser/                  # PgSQL 有线协议消息解析器
│   ├── pgpool/                    # PgSQL 连接池（基于 pgxpool）
│   ├── pgproxy/                   # PgSQL TCP 代理（基于 pgproto3）
│   ├── mysqlpool/                 # MySQL 连接池（基于 database/sql）
│   ├── mysqlproxy/                # MySQL TCP 代理 + 自研有线协议
│   ├── httpclient/                # 代理到服务器的 HTTP 客户端
│   ├── handler/                   # 框架无关的核心处理器
│   │   └── adapter/               # Gin / Fiber 路由注册器
│   └── httphandler/               # 旧版 Gin 耦合处理器（已弃用）
├── internal/config/               # 共享配置
├── test/
│   ├── integration/               # 集成测试（需数据库）
│   └── e2e/                       # 端到端测试（全面 SQL 测试）
├── assets/                        # Logo 资产（SVG + ASCII）
├── docker-compose.yml             # 生产 Docker Compose
├── docker-compose.test.yml        # 测试 Docker Compose
├── Dockerfile                     # 多阶段构建
├── magefile.go                    # Mage 构建系统
├── config.json                    # 生产配置
├── config.test.json               # 测试配置
└── go.mod
```

---

## 构建系统

项目使用 [Mage](https://magefile.org/) 替代 Makefile：

```bash
# 安装 Mage
go install github.com/magefile/mage@latest

# 可用命令
mage build                  # 编译 server 和 proxy 二进制
mage test                   # 运行所有单元测试
mage testintegration        # 运行集成测试（需 Docker）
mage lint                   # go vet ./...
mage release                # 交叉编译 linux/darwin/windows × amd64/arm64
mage dockerUp               # docker compose up -d（测试环境）
mage dockerDown             # docker compose down --volumes
mage dockerBuild            # docker compose build --no-cache
mage ci                     # 完整 CI 流水线
mage all                    # Lint → test → build
mage clean                  # 清理构建产物
```

---

## 测试

### Docker 测试环境

使用 `docker-compose.test.yml` 启动测试环境，包含以下服务：

| 服务 | 镜像 | 端口映射 (宿主机:容器) | 账号密码 |
|------|------|----------------------|----------|
| **PostgreSQL** | postgres:14-alpine | `25432:5432` | 用户=`postgres` / 密码=`postgres` / 库=`testdb` |
| **MySQL** | mysql:8.0 | `23306:3306` | 用户=`root` / 密码=`root` / 库=`testdb` |
| **Server** | 自建 | `28080:8080` | 加密密钥=`http-pg-test-key` |
| **Proxy** (PG+MySQL) | 自建 | `26543:6543` (PG), `23307:3306` (MySQL) | 加密密钥=`http-pg-test-key` |

测试配置文件 `config.test.json` 自动连接到以上端口。

```bash
# 启动测试环境（直接使用 docker compose）
docker compose -f docker-compose.test.yml up -d

# 验证所有服务健康
curl http://localhost:28080/api/v1/health

# 停止并清理（清除数据库数据）
docker compose -f docker-compose.test.yml down --volumes
```

或通过 mage 命令：

```bash
mage dockerUp
mage dockerDown
```

### 单元测试

```bash
go test ./pkg/... -v
```

### 集成测试（需要 Docker 数据库）

```bash
mage dockerUp
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
mage dockerDown
```

### 端到端测试（完整代理链路）

E2E 测试会自动启动 server+proxy（复用 Docker PostgreSQL 和 MySQL 数据库）：

```bash
# 同时运行 PgSQL 和 MySQL E2E 测试
mage dockerUp
go test -v ./test/e2e/ -timeout 120s
mage dockerDown

# 仅运行 PgSQL 测试
go test -v -run 'TestPgSQLComprehensive' ./test/e2e/ -timeout 120s

# 仅运行 MySQL 测试
go test -v -run 'TestMySQLComprehensive' ./test/e2e/ -timeout 120s
```

### 测试覆盖

| 层级 | 包 | 测试数 | 依赖 |
|------|------|--------|------|
| 单元 | `pkg/crypto` | 5 | 无 |
| 单元 | `pkg/pgparser` | 9 | 无 |
| 单元 | `pkg/pgpool` | 2 | Docker PostgreSQL |
| 单元 | `pkg/httpclient` | 5 | 无 |
| 单元 | `pkg/httphandler` | 7 | 无 |
| 单元 | `pkg/handler` | 20 | 无 |
| 单元 | `pkg/pgproxy` | 4 | 无 |
| 单元 | `pkg/mysqlpool` | 2 | Docker MySQL |
| 单元 | `pkg/mysqlproxy` | 15 | 无 |
| 集成 | `test/integration` | 2 | `mage dockerUp` + `-tags integration` |
| E2E — PgSQL | `test/e2e` | 9 个子测试 | `mage dockerUp` |
| E2E — MySQL | `test/e2e` | 9 个子测试 | `mage dockerUp` |

全部 69 个单元测试通过：

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

## 有线协议实现细节

### PgSQL（基于 pgx/pgproto3）

- 完整支持 PostgreSQL wire protocol v3
- 简单查询（`'Q'`）和扩展查询协议（Parse/Bind/Describe/Execute/Sync）
- RowDescription 使用文本格式，DataRow 包含原始值
- 使用 `github.com/jackc/pgx/v5/pgproto3` 编解码消息

### MySQL（自研实现）

- 无外部协议库依赖，纯手写实现
- HandshakeV10、HandshakeResponse41、COM_QUERY、COM_PING、COM_QUIT、COM_INIT_DB
- 文本协议结果集：列计数 → ColumnDef×N → EOF → 数据行×N → EOF
- 按 MySQL 规范实现长度编码整数和字符串编码
- 所有状态管理命令（COM_STMT_PREPARE/EXECUTE/CLOSE）返回错误

---

## 依赖库

- [pgx v5](https://github.com/jackc/pgx) — PostgreSQL 驱动、pgproto3、pgxpool
- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) — MySQL 驱动
- [Gin](https://github.com/gin-gonic/gin) — HTTP Web 框架（默认）
- [Fiber v3](https://github.com/gofiber/fiber) — HTTP Web 框架（可选，通过 `-tags fiber`）
- [google/uuid](https://github.com/google/uuid) — UUID 生成

---

## 许可证

MIT
