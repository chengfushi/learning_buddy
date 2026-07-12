---
name: backend-conventions
description: Go 后端（learning_buddy/backend）开发规范——分层架构、错误处理、GORM 查询、权限铁律、命名与注释、工具链。编写或评审 backend/ 代码时自动遵循。
---

# Backend 开发规范（Go / Gin / GORM）

适用目录：`backend/`。技术栈：Go 1.25 + Gin + GORM + PostgreSQL/pgvector。

## 0. 架构铁律

### 0.1 分层依赖（单向：handler → service → repository）

```
backend/internal/
├── config/       # 环境变量 / 配置解析
├── handler/      # Gin 路由注册 + 请求参数校验，禁止写业务/权限 SQL
├── middleware/    # JWT 鉴权 + RBAC（RequireRole）
├── model/        # GORM 模型（纯字段映射），禁止写查询方法
├── repository/   # 仅此层拼 SQL / GORM；权限谓词唯一真源
└── service/      # 业务逻辑编排、RBAC，不直接拼「资料可见性」SQL
```

- **禁止反向依赖**：repository 不能 import service；service 不能 import handler。
- 新增功能按层添加：先在 model 定义表结构 → repository 写查询方法 → service 编排 → handler 暴露路由。

### 0.2 权限铁律（最高优先级）

> **凡涉及「用户可见资料范围」的逻辑，只能写在后端 `repository` 层。Agent 与前端永远不直接拼权限谓词。**

RAG 检索谓词（repository 构建后下发）：
```sql
team_id IN (可见 team 集合) AND (teams.type <> 'teacher' OR materials.shared = true)
```
- `shared` 字段仅对 `teacher` team 生效；学生私有资料、public 资料的 `shared` 写入必须被忽略/拒绝。
- 权限相关改动**必须带测试**（单测覆盖谓词，集成测试覆盖完整 RAG 权限流）。
- `VisibleTeamIDs()` + `VisibleMaterialsScope()` 是「用户可见资料范围」的**唯一真源**，任何新入口必须复用。

## 1. Package 与文件

### 1.1 Package 注释

每个 package 的第一行必须是用途说明：

```go
// package repository —— 仅此层拼 SQL / GORM。
// 权限铁律（engineering-standards.md §0）：任何「用户可见资料范围」的逻辑只能写在这里。
package repository
```

### 1.2 文件组织

- 一个概念一个文件，避免 `all.go` / `utils.go` 巨型文件。
- 模型文件放在 `model/` 下按领域拆分（`models.go` 放核心实体，`types.go` 放枚举/辅助类型）。
- handler 按领域拆分（`auth.go`, `materials.go`, `teams.go`, `agent.go` 等）。

## 2. 编码规范

### 2.1 风格

遵循 [uber-go/guide](https://github.com/uber-go/guide)。统一使用 `gofmt` + `goimports` 格式化。

### 2.2 命名

- Go 标准导出规则：大写开头 = 导出；小写开头 = 包内私有。
- 中文注释中的专有名词：`team` → 团队/知识库，`material` → 资料，`shared` → 是否对学生可见。
- 变量/函数名用英文（Go 惯例），注释可以中文描述领域语义。
- 缩写保持全大写或全小写：`ID`（不是 `Id`）、`URL`、`DB`。
- GORM 模型用 snake_case 映射列名（`gorm:"column:display_name"`）。

### 2.3 错误处理

```go
// ✅ 正确：包装上下文
return fmt.Errorf("list materials: %w", err)

// ✅ 正确：service 层处理业务错误
if !hasAccess {
    return nil, ErrForbidden
}

// ❌ 错误：裸 return
return err

// ❌ 错误：handler 层 panic
panic("unexpected")
```

- handler 层禁止 `panic`；所有错误通过 Gin `c.JSON()` 返回。
- 使用 `errors.Is` / `errors.As` 做错误类型判断。
- 自定义错误类型（`ErrForbidden`, `ErrNotFound`）定义在出错的 package 内。

### 2.4 Context 传递

```go
// ✅ 正确：透传 context
func (r *Repositories) VisibleTeamIDs(ctx context.Context, userID int64) ([]int64, error) {
    err := r.DB.WithContext(ctx).Table("teams")...

// ❌ 错误：请求路径中用 context.Background()
err := r.DB.WithContext(context.Background()).Table("teams")...
```

- 所有 DB / Redis / HTTP 调用必须接收并传递 `ctx`。
- Gin handler 中的 `c.Request.Context()` 自动携带超时/取消/trace。

### 2.5 日志

```go
import "log/slog"

slog.Info("material uploaded", "id", id, "team", teamID)
slog.Error("failed to parse", "err", err, "material", id)
```

- 用 `log/slog` 结构化日志，禁止 `fmt.Println` / `println`。
- 敏感字段（密码、token）**必须脱敏**后输出。
- 关键路径（鉴权、RAG 检索、Agent 调用）记录带 key-value 对的结构化日志。

### 2.6 GORM 查询

```go
// ✅ 正确：Where + Limit(1) + Find
var m model.Material
err := db.Where("id = ? AND team_id = ?", id, teamID).Limit(1).Find(&m).Error

// ❌ 错误：db.First / db.Take（主键顺序不可靠）
err := db.First(&m, id)

// ✅ 正确：批量查询避免 N+1
var teams []model.Team
err := db.Where("id IN ?", teamIDs).Find(&teams).Error

// ❌ 错误：循环中逐条查询
for _, id := range ids {
    db.Where("id = ?", id).First(&t)  // N+1
}
```

- 禁止使用 `db.First` / `db.Take`（依赖主键排序，不可靠）。
- 禁止循环中逐条 DB 查询（N+1）。
- 写操作显式使用事务：`db.Transaction(func(tx *gorm.DB) error { ... })`。
- 权限/计费关键路径优先用 `sqlc` 生成类型安全 SQL（减少 GORM 魔法）。

### 2.7 类型与 Tag

```go
// GORM 模型示例
type Material struct {
    ID          int64     `gorm:"primaryKey;column:id"`
    TeamID      int64     `gorm:"column:team_id;index"`
    Title       string    `gorm:"column:title"`
    Shared      bool      `gorm:"column:shared;default:false"`
    ParseStatus string    `gorm:"column:parse_status;default:pending"` // pending→parsing→done/failed
    CreatedAt   time.Time `gorm:"column:created_at"`
}
```

- 每个 GORM 模型**必须**定义 `TableName()` 方法返回实际表名。
- 列名用 snake_case 显式标注（`column:xxx`），不依赖 GORM 默认命名。
- 外键/索引/唯一约束在 tag 中明确声明。
- 业务数据序列化不依赖 GORM tag — 用独立的 DTO 或 `SetFromBusinessData` / `ToBusinessData` 方法。

### 2.8 中间件与路由

```go
// ✅ 新路由必须声明 RBAC 中间件
auth.POST("/teams", middleware.RequireRole("teacher"), h.createTeam)
auth.POST("/teams/:id/join", middleware.RequireRole("student"), h.joinTeam)

// ✅ 公开接口不加 RBAC
api.POST("/auth/login", h.login)
```

- 新路由默认 deny-all（必须显式加 `RequireRole(...)`）。
- `RequireRole` 必须在 `AuthMiddleware` 之后（依赖注入的 `role`）。
- 从 Context 取用户信息用 `middleware.CtxUserID(c)` 和 `middleware.CtxRole(c)`。

## 3. 测试规范

```go
// repository 层测试
func TestVisibleTeamIDs_StudentOnlySeesOwnAndApproved(t *testing.T) {
    // 断言学生的可见集不包含 teacher 未批准 team
}
```

- 权限/计费路径测试覆盖率 ≥ 90%（CI 门禁阻断）。
- 整体覆盖率 ≥ 70%。
- 权限测试**必须包含反向用例**：断言学生看不到 `shared=false` 的 teacher 资料。
- 使用 `testify` 断言库，测试文件命名 `*_test.go`。

## 4. 工具链

| 工具 | 用途 | 配置 |
|------|------|------|
| `gofmt -w .` | 格式化 | Go 标准 |
| `go vet ./...` | 静态分析 | Go 内置 |
| `golangci-lint run` | Lint（govet + staticcheck + errcheck + gosec + ineffassign + unused） | `backend/.golangci.yml` |
| `go test -cover ./...` | 单测 + 覆盖率 | - |
| `gosec` | 安全扫描 | 集成在 golangci-lint 中 |

`.golangci.yml` 启用的 linter：
- `govet` — 可疑构造（fat 复制、错位格式串）
- `staticcheck` — 静态分析（死代码、简化写法）
- `errcheck` — 未处理的 error 返回值
- `ineffassign` — 写后未读的赋值
- `unused` — 未使用的变量/函数/类型
- `gosec` — 安全漏洞（SQL 注入、硬编码密钥；测试文件豁免）

## 5. 常见模式

### 5.1 新增 API 端点流程

1. `model/` → 定义请求/响应辅助结构（或复用已有 DTO）
2. `repository/` → 添加数据访问方法（含权限谓词）
3. `service/` → 编排业务逻辑，调用 repository
4. `handler/` → 注册路由 + 解析参数 + 调用 service + 返回 JSON
5. `handler/router.go` → 注册路由 + `RequireRole`
6. `*_test.go` → 单测（repository 层必须有权限测试）
7. 同步 `docs/system-design.md`（API 变更时）

### 5.2 配置管理

```go
// backend/internal/config/config.go
type Config struct {
    DBDSN         string
    EmbeddingDim  int
    AgentBaseURL  string
    // ... 全部从环境变量读取
}
```

- 配置集中到 `config/config.go`，不散落到各模块。
- 使用 `os.Getenv` + 默认值模式。

## 6. 禁止清单

- ❌ handler / service 层拼权限 SQL（必须走 repository）
- ❌ `db.First` / `db.Take`（用 `Where + Limit(1) + Find`）
- ❌ handler 层 `panic`
- ❌ 循环中逐条 DB 查询（N+1）
- ❌ 请求路径中用 `context.Background()`
- ❌ `fmt.Println` / `println` 记录日志（用 `slog`）
- ❌ 硬编码密钥/密码/Token（用 env / config）
- ❌ 无测试的权限/计费改动
- ❌ repository ← service / service ← handler 反向依赖
- ❌ 提交带 `golangci-lint` error 的代码
