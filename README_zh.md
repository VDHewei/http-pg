# HTTP-PG: PgSQL 代理通过 HTTP 加密接口转发

```
  __    __  ________  ________  _____       ________  ________
 / /   / / /_  __/ / /_  __/ / / ___ \     / ___/ _ \/ ___/ _ \
/ /_  / /   / / / /   / /   / / /_/ /    / (_ /  __/ (_ / , _/
\___/ /_/   /_/ /_/   /_/   /_/\____/     \___/\___/\___/_/|_|

      HTTP-PG  PgSQL 代理 · HTTP 加密传输
```

一个将 PgSQL 有线协议消息通过 HTTP API 服务器转发的 PostgreSQL 代理，使用 AES-256-GCM 加密。服务器解密消息后，在真实的 PostgreSQL 连接上执行 SQL，并通过加密的 HTTP 通道返回结果。

## 架构

```
PgSQL 客户端  --[TCP/有线协议]-->  代理  --[HTTPS/AES-GCM]-->  HTTP 服务器  -->  PostgreSQL
```

## 特性

- **PgSQL 有线协议**: 通过 `pgx/pgproto3` 完整支持 PostgreSQL 有线协议 v3
- **端到端加密**: 所有 HTTP 流量使用 AES-256-GCM 认证加密
- **连接池管理**: 通过 `pgxpool` 实现高效的 PostgreSQL 连接管理
- **会话管理**: 每个客户端连接使用 UUID 进行会话跟踪
- **SQL 解析**: 从有线协议消息中提取并记录 SQL 语句
- **可扩展架构**: 代理、客户端、处理器、连接池各层清晰分离

## 快速开始

### 前置条件

- Go 1.25+
- PostgreSQL 14+

### 安装

```bash
git clone https://github.com/http-pg/http-pg.git
cd http-pg
go mod tidy
```

### 配置

```bash
# 生成加密密钥
export ENCRYPTION_KEY="your-secure-encryption-key-at-least-32-chars"
```

编辑 `config.json`:
```json
{
    "server_addr": ":8080",
    "proxy_addr": ":5432",
    "postgres_dsn": "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
    "max_connections": 20,
    "min_connections": 5
}
```

### 运行

启动 HTTP API 服务器:
```bash
go run ./cmd/server -config config.json
```

启动 PgSQL 代理:
```bash
go run ./cmd/proxy -config config.json
```

使用任意 PgSQL 客户端连接:
```bash
psql -h localhost -p 5432 -U postgres
```

### Docker

```bash
docker-compose up -d
```

## API 端点

| 方法 | 路径 | 描述 |
|--------|------|-------------|
| GET | `/api/v1/health` | 健康检查及连接池状态 |
| POST | `/api/v1/session` | 创建新的 PgSQL 会话 |
| POST | `/api/v1/query` | 执行 SQL 查询 |
| DELETE | `/api/v1/session/:id` | 关闭会话 |

## 配置参数

| 参数 | 环境变量 | 默认值 | 描述 |
|-----------|---------|---------|-------------|
| `server_addr` | - | `:8080` | HTTP 服务器监听地址 |
| `proxy_addr` | - | `:5432` | PgSQL 代理监听地址 |
| `postgres_dsn` | `POSTGRES_DSN` | `localhost:5432` | PostgreSQL 连接字符串 |
| `encryption_key` | `ENCRYPTION_KEY` | *必填* | AES-256-GCM 加密密钥 |
| `max_connections` | - | `20` | 最大 PG 连接数 |
| `min_connections` | - | `5` | 最小 PG 连接数 |

## 项目结构

```
http-pg/
├── cmd/
│   ├── proxy/main.go          # PgSQL 代理入口
│   └── server/main.go         # HTTP API 服务器入口
├── pkg/
│   ├── crypto/                # AES-256-GCM 加密/解密
│   ├── pgparser/              # PgSQL 有线协议解析器
│   ├── pgpool/                # PgSQL 连接池管理器
│   ├── pgproxy/               # PgSQL TCP 代理
│   ├── httpclient/            # 代理端 HTTP 客户端
│   └── httphandler/           # Gin HTTP API 处理器
├── internal/config/           # 共享配置
├── test/integration/          # 集成测试
├── docker-compose.yml         # Docker 部署
└── config.json                # 配置文件
```

## 测试

```bash
# 单元测试
go test ./pkg/... -v

# 集成测试（需要 PostgreSQL）
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
```

## 依赖库

- [pgx](https://github.com/jackc/pgx) - PostgreSQL 驱动及工具包
- [Gin](https://github.com/gin-gonic/gin) - HTTP Web 框架
- [UUID](https://github.com/google/uuid) - UUID 生成

## 许可证

MIT
