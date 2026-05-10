# HTTP-PG 架构升级 - 任务分解

## 已完成阶段

### Phase 1: Foundation
- [x] Task 1: 项目结构初始化、go mod、配置系统
- [x] Task 2: AES-256-GCM 加解密层
- [x] Task 3: PgSQL 消息解析器（pgproto3 ↔ SQL）

### Phase 2: Core
- [x] Task 4: PgSQL 连接池（pgxpool）
- [x] Task 5: HTTP 客户端消息转发
- [x] Task 6: HTTP API 处理器（Gin）

### Phase 3: Integration
- [x] Task 7: PgSQL TCP 代理
- [x] Task 8: 服务器入口
- [x] Task 9: 集成测试

### Phase 4: Polish
- [x] Task 10: 项目 Logo
- [x] Task 11: README（zh, en, zh-TW）
- [x] Task 12: Git 初始化与提交
- [x] Task 13: pgproxy 单元测试和集成测试完善

---

## Phase 5: 架构升级（进行中）

### Task 14: HTTP Handler 核心逻辑抽离

**目标**：将 `pkg/httphandler/handler.go` 中与 Gin 耦合的代码分离，抽取出框架无关的核心业务逻辑。

**文件变更**：
- [ ] 14.1 创建 `pkg/handler/types.go` - 共享请求/响应类型定义
  - `SessionRequest` - 会话创建请求
  - `QueryRequest` - SQL 查询请求
  - `QueryResponse` - SQL 查询响应
  - `ProtocolType` - 协议类型枚举
  - `HealthStatus` - 健康状态
- [ ] 14.2 创建 `pkg/handler/handler.go` - 框架无关核心处理器
  - `NewHandler()` - 创建处理器实例
  - `CreateSession()` - 创建会话（解密 + 池获取 + 返回 sessionID）
  - `HandleQuery()` - 处理查询（解密 → SQL 路由 → 执行 → 加密响应）
  - `CloseSession()` - 关闭会话
  - `HealthCheck()` - 健康检查
  - `executeSQL()` - SQL 执行路由
  - `executeQuery()` - 查询类型执行
  - `executeCommand()` - 命令类型执行
  - `sqlCommandType()` - SQL 命令类型提取
- [ ] 14.3 创建 `pkg/handler/handler_test.go` - 核心逻辑单元测试
- [ ] 14.4 重构 `cmd/server/main.go` - 使用新的核心 handler
- [ ] 14.5 废弃 `pkg/httphandler/`（最终删除）

### Task 15: Gin + Fiber 双框架支持

**目标**：通过 Go build tags 实现编译时框架选择（Gin 默认，Fiber 使用 `-tags fiber`）。

**文件变更**：
- [ ] 15.1 创建 `pkg/handler/adapter/gin.go`（build tag: `!fiber`）
  - `RegisterGinRoutes()` - 将 Handler 方法注册到 Gin 路由
  - Gin 中间件函数：ginCreateSession、ginHandleQuery、ginCloseSession、ginHealthCheck
- [ ] 15.2 创建 `pkg/handler/adapter/fiber.go`（build tag: `fiber`）
  - `RegisterFiberRoutes()` - 将 Handler 方法注册到 Fiber 路由
  - Fiber 处理器函数：fiberCreateSession、fiberHandleQuery、fiberCloseSession、fiberHealthCheck
- [ ] 15.3 修改 `cmd/server/main.go`
  - 拆分为 `main_gin.go` (build: `!fiber`) 和 `main_fiber.go` (build: `fiber`)
  - `setupRouter()` 函数在各自文件中实现
- [ ] 15.4 更新 `go.mod` - 添加 `github.com/gofiber/fiber/v3` 依赖
- [ ] 15.5 验证两种构建方式均能编译通过

### Task 16: HTTP 客户端路由可选配置

**目标**：代理转发 HTTP 请求时的服务端 URL 支持配置化，不再硬编码 `localhost`。

**文件变更**：
- [ ] 16.1 修改 `internal/config/config.go`
  - 新增字段：`ServerURL`（HTTP 服务器完整 URL）
  - 新增字段：`ProxyProtocol`（pg / mysql / both）
  - 新增字段：`MySQLProxyAddr`（MySQL 代理监听地址）
  - 新增字段：`MySQLDSN`（MySQL 数据源连接串）
  - 默认值回退逻辑（`server_url` 为空时自动拼接）
- [ ] 16.2 修改 `pkg/httpclient/client.go`
  - 新增 `ClientConfig` 结构体（BaseURL、Timeout、RetryAttempts）
  - `NewClient` 接受 `ClientConfig` 参数
- [ ] 16.3 修改 `cmd/proxy/main.go` - 使用配置化的 URL
- [ ] 16.4 更新 `config.json` - 添加新配置项及注释

### Task 17: 转发服务 SQL 执行结果全面测试

**目标**：覆盖所有 SQL 命令类型的执行结果测试，确保转发服务正确处理各种 SQL 场景。

**文件变更**：
- [ ] 17.1 创建 `pkg/handler/handler_test.go` - 核心处理器测试（~20 个测试函数）
  - 会话 CRUD 测试：创建、查询、关闭、异常关闭
  - 查询类型：SELECT（多列/多行/空结果）、SHOW、EXPLAIN、WITH
  - 命令类型：INSERT、UPDATE（0/N 行）、DELETE、TRUNCATE
  - DDL：CREATE TABLE/INDEX、ALTER TABLE、DROP TABLE/INDEX
  - 事务：BEGIN、COMMIT、ROLLBACK
  - 配置：SET 命令
  - 边界值：NULL 值、空字符串、特殊字符、大结果集
  - 错误场景：SQL 语法错误、表不存在、连接池错误、解密失败、会话不存在
- [ ] 17.2 扩展 `pkg/httpclient/client_test.go`
  - 加密解密往返测试
  - 大负载测试
  - 超时测试
  - 重试逻辑测试
  - 服务端错误响应测试
- [ ] 17.3 扩展 `pkg/pgproxy/proxy_test.go`
  - 大结果集（1000+ 行）
  - 二进制数据（BYTEA）
  - 浮点精度（float4/float8）
  - 并发客户端连接
  - 扩展查询协议（Parse/Bind/Execute）
  - 客户端异常断开
  - HTTP 服务不可达

### Task 18: MySQL 协议支持

**目标**：在同一代理架构下新增 MySQL 协议支持，复用 HTTP 转发、加密、会话管理基础设施。

**依赖**：`github.com/go-sql-driver/mysql`（数据库驱动）+ 自实现 MySQL 协议编码器

**文件变更**：
- [ ] 18.1 创建 `pkg/mysqlparser/types.go` - MySQL 协议类型定义
  - `HandshakeV10` 结构体
  - `HandshakeResponse41` 结构体
  - 能力标志位常量（ClientLongPassword、ClientPluginAuth 等）
  - 命令类型常量（COM_QUERY=0x03、COM_QUIT=0x01 等）
- [ ] 18.2 创建 `pkg/mysqlparser/auth.go` - MySQL 认证处理
  - `EncodeHandshakeV10()` - 编码服务端握手包
  - `DecodeHandshakeResponse41()` - 解码客户端握手响应
  - `VerifyNativePassword()` - mysql_native_password 认证验证
- [ ] 18.3 创建 `pkg/mysqlparser/encoder.go` - MySQL 协议编码
  - `EncodeOKPacket()` - 编码成功响应包
  - `EncodeErrPacket()` - 编码错误响应包
  - `EncodeColumnDefinition()` - 编码列定义包
  - `EncodeResultSetRow()` - 编码结果集数据行
  - `EncodeEOF()` - 编码结果集结束标记
- [ ] 18.4 创建 `pkg/mysqlparser/parser.go` - MySQL 协议解析
  - `ReadPacket()` - 读取一个完整的 MySQL 包
  - `DecodeCommand()` - 解码客户端命令
  - `ExtractSQLFromQuery()` - 从 COM_QUERY 提取 SQL
- [ ] 18.5 创建 `pkg/mysqlparser/parser_test.go` - 解析器测试
- [ ] 18.6 创建 `pkg/mysqlpool/pool.go` - MySQL 连接池
  - `PoolManager` 结构体（使用 `database/sql` + mysql driver）
  - `New()` - 创建连接池
  - `AcquireSession()` / `ReleaseSession()` - 会话连接管理
  - `ExecSQL()` - SELECT 类查询
  - `ExecCommand()` - DML/DDL 命令
- [ ] 18.7 创建 `pkg/mysqlpool/pool_test.go` - 连接池测试
- [ ] 18.8 重构 `pkg/pgproxy/proxy.go` - 抽取通用代理框架
  - 定义 `ProtocolHandler` 接口（HandleConnection、ProtocolType）
  - `Proxy` 结构体通用化（listener、handler、httpClient）
  - `New()` / `Start()` / `Stop()` 保持不变
- [ ] 18.9 创建 `pkg/pgproxy/pg_handler.go` - PgSQL 协议处理器
  - 将现有 `handleConnection` 逻辑迁移为 `PgHandler.HandleConnection()`
- [ ] 18.10 创建 `pkg/pgproxy/mysql_handler.go` - MySQL 协议处理器
  - `MySQLHandler` 实现 `ProtocolHandler` 接口
  - 握手流程：发送 HandshakeV10 → 读取 HandshakeResponse41 → 发送 OK
  - 命令循环：读取命令 → COM_QUERY 转发 → 结果编码返回 → COM_QUIT 断开
  - `sendQueryResult()` - 将 HTTP 响应编码为 MySQL 结果集
  - `sendError()` - 编码并发送错误响应
- [ ] 18.11 修改 `pkg/handler/handler.go` - 扩展双池支持
  - `Handler` 新增 `mysqlPool` 字段
  - `CreateSession()` 根据协议类型路由到对应池
  - `HandleQuery()` 根据协议类型执行 `executePgSQL` / `executeMySQL`
  - `executeMySQL()` - MySQL 查询执行
- [ ] 18.12 修改 `cmd/proxy/main.go` - 双协议代理启动
  - 根据配置启动 PgSQL 和/或 MySQL 代理
  - 优雅关闭所有代理
- [ ] 18.13 修改 `cmd/server/main.go` - 初始化双池
  - 同时初始化 PgSQL 和 MySQL 连接池
  - 传递给 Handler
- [ ] 18.14 更新 `go.mod` - 添加 MySQL 驱动依赖

### Task 19: README 文档更新

**目标**：全面更新所有语言版本的 README，反映新架构、新功能和使用方式。

**文件变更**：
- [ ] 19.1 更新 `README.md`（英文版）
  - 标题和简介（双协议支持）
  - 架构图（双协议数据流）
  - Features（MySQL、双框架、可配置路由、全面测试）
  - 快速开始（MySQL 代理启动、Fiber 构建）
  - 配置说明（新增字段）
  - 项目结构（新目录）
  - 构建方式（Gin vs Fiber）
  - API 文档（协议类型参数）
- [ ] 19.2 更新 `README_zh.md`（中文版）
- [ ] 19.3 更新 `README_zh-TW.md`（繁体中文版）

### Task 20: Logo 优化重设计

**目标**：重新设计 Logo 为现代扁平风格，输出到 `docs/logo.svg`。

**要求**：
- [ ] 20.1 设计现代扁平风格 SVG Logo（400×400 视口）
  - 深色背景（#1a1a2e）
  - PgSQL 大象简化几何图形（#336791）
  - MySQL 海豚简化几何图形（#F29111）
  - 双向连接箭头（#7ec8e3 / #00bcd4）
  - HTTP-PG 文字标题
  - DATABASE PROXY 副标题
- [ ] 20.2 输出到 `docs/logo.svg`
- [ ] 20.3 更新 README 中的 Logo 引用路径

---

## 实现顺序

```
阶段一：基础重构
  Task 14 (Handler 抽离) → Task 15 (双框架) → Task 16 (配置化)
     ↓
阶段二：测试（可并行）
  Task 17 (全面测试)
     ↓
阶段三：MySQL（依赖阶段一）
  Task 18.1-18.7 (MySQL 基础设施) → Task 18.8-18.10 (协议处理器) → Task 18.11-18.14 (集成)
     ↓
阶段四：文档与美化（最后）
  Task 19 (README) + Task 20 (Logo)
```

## 如何构建和测试

```bash
# Gin 版本（默认）
go build ./cmd/server
go build ./cmd/proxy

# Fiber 版本
go build -tags fiber ./cmd/server

# 运行所有测试
go test ./pkg/... -v -count=1

# 指定包测试
go test ./pkg/handler/... -v -count=1
go test ./pkg/pgproxy/... -v -count=1

# 集成测试（需要真实数据库）
INTEGRATION_TEST=1 go test -tags integration ./test/... -v
```
