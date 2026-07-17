# 智能学伴系统 · API 接口文档

> 版本：v0.3 · 最后更新：2026-07-12
> 对应代码：`backend/internal/handler/` · `backend/internal/model/` · `backend/internal/middleware/`

---

## 目录 / Index

| 模块 | 端点数 | 说明 |
|------|--------|------|
| [认证 / Auth](#1-认证--auth) | 4 | 注册、登录、刷新 token、当前用户 |
| [团队 / Teams](#2-团队--teams) | 7 | 创建学习小组、加入/审批、成员管理、team 内资料 |
| [资料 / Materials](#3-资料--materials) | 6 | 资料 CRUD、可见性过滤、异步解析与失败重试 |
| [笔记 / Notes](#4-笔记--notes) | 4 | 阅读器笔记与标注 |
| [学习记录 / Learning](#5-学习记录--learning) | 3 | 学习时长、进度记录、进度聚合 |
| [Agent / AI](#6-agent--ai) | 6 | 流式答疑、学习计划、智能测评、会话管理 |

---

## 通用约定 / Conventions

### 鉴权 / Authentication

所有 `/api/*` 鉴权接口需要在请求头中携带 Bearer Token：

```
Authorization: Bearer {access_token}
```

- **access_token** 有效期 24 小时，由登录/注册接口返回。
- **refresh_token** 有效期 7 天，通过 `/api/auth/refresh` 换发新 access_token。
- 注册时自动创建学生私有 team（同一事务，保证原子性）。
- JWT 载荷：`{uid, role, sub, iat, exp}`，签名算法 HS256。

### 角色 / Roles

| 角色 | 值 | 说明 |
|------|-----|------|
| 学生 | `student` | 自动拥有私人 team，可加入老师 team（需审批） |
| 老师 | `teacher` | 可创建学习小组，仅老师能在自己 team 上传资料 |
| 超级管理员 | `super_admin` | 维护公共库，全平台可见。**只能通过数据库种子写入，禁止通过 API 注册** |

### 响应格式 / Response Format

所有成功响应为 JSON，字段名与 Go 结构体对齐（PascalCase，无 json tag 的 GORM 默认行为）：

```json
{
  "user": { "ID": 1, "Email": "alice@example.com", ... }
}
```

错误响应：

```json
{ "error": "错误描述" }
```

### 幂等性 / Idempotency

| Method | 幂等 |
|--------|------|
| `GET` | ✅ 是 |
| `PUT` | ✅ 是 |
| `DELETE` | ✅ 是 |
| `POST` | ❌ 否（每次请求创建新资源） |

---

## 1. 认证 / Auth

### POST /api/auth/register

注册新用户。成功后自动登录并返回 JWT。注册时自动在**同一事务中**创建学生/老师的私有 team（F2.3）。

**鉴权**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | ✅ | 登录邮箱，全平台唯一 |
| `password` | string | ✅ | 密码，最少 6 位 |
| `display_name` | string | ✅ | 显示名称 |
| `role` | string | | 角色：`student`（默认）/ `teacher`。`super_admin` 禁止通过 API 注册 |

**Response**

#### 200 OK

```json
{
  "user": {
    "id": 1,
    "email": "alice@example.com",
    "display_name": "Alice",
    "role": "student",
    "subscription": "free",
    "created_at": "2026-07-12T10:00:00Z"
  },
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 邮箱或密码为空、密码不足 6 位、不允许注册的角色 |
| 400 | 邮箱已存在（数据库唯一约束） |

---

### POST /api/auth/login

登录并返回 JWT。

**鉴权**：否

**幂等**：否（每次生成新 token）

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `email` | string | ✅ | 登录邮箱 |
| `password` | string | ✅ | 密码 |

**Response**

#### 200 OK

```json
{
  "user": {
    "id": 1,
    "email": "alice@example.com",
    "display_name": "Alice",
    "role": "student",
    "subscription": "free",
    "created_at": "2026-07-12T10:00:00Z"
  },
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 请求格式错误 |
| 401 | 账号或密码错误 |

---

### POST /api/auth/refresh

用 refresh_token 换发新的 access_token。

**鉴权**：否

**幂等**：否（每次生成新 token）

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `refresh_token` | string | ✅ | 登录时返回的 refresh_token |

**Response**

#### 200 OK

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 缺少 refresh_token |
| 401 | refresh_token 无效或已过期 |

---

### GET /api/me

获取当前登录用户信息（脱敏，不返回 `password_hash`）。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Response**

#### 200 OK

```json
{
  "user": {
    "id": 1,
    "email": "alice@example.com",
    "display_name": "Alice",
    "role": "student",
    "subscription": "free",
    "created_at": "2026-07-12T10:00:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录或 token 失效 |
| 404 | 用户不存在 |

---

## 2. 团队 / Teams

### GET /api/teams

获取当前用户可见的 team 列表。可见集合 = 私人 team（自己是 owner）+ 已 `approved` 加入的 teacher team + 公共库（`type='public'`）。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Response**

#### 200 OK

```json
{
  "teams": [
    {
      "ID": 1,
      "Name": "Alice 的私有资料",
      "Type": "private",
      "JoinCode": null,
      "OwnerID": 1,
      "CreatedAt": "2026-07-12T10:00:00Z"
    },
    {
      "ID": 2,
      "Name": "数学提高班",
      "Type": "teacher",
      "JoinCode": "A3FK9M",
      "OwnerID": 2,
      "CreatedAt": "2026-07-12T11:00:00Z"
    },
    {
      "ID": 0,
      "Name": "公共知识库",
      "Type": "public",
      "JoinCode": null,
      "OwnerID": null,
      "CreatedAt": "2026-07-11T00:00:00Z"
    }
  ]
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |

---

### POST /api/teams

老师创建学习小组 team，自动生成 6 位唯一 `join_code`（F2.1）。

**鉴权**：是（仅 `teacher` 角色）

**幂等**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | 团队名称 |

**Response**

#### 200 OK

```json
{
  "id": 2,
  "name": "数学提高班",
  "type": "teacher",
  "join_code": "A3FK9M",
  "owner_id": 2
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 团队名称为空 |
| 401 | 未登录 |
| 403 | 非 teacher 角色 |

---

### POST /api/teams/:id/join

学生凭团队 ID 申请加入 teacher team，状态为 `pending`（待审批）。该接口内部通过 `join_code` 完成加入逻辑，团队必须为 teacher 类型且有有效的 `join_code`。

**鉴权**：是（仅 `student` 角色）

**幂等**：是（已存在的成员关系直接返回，不重复创建）

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 团队 ID |

**Response**

#### 200 OK

```json
{
  "status": "pending",
  "team_id": 2
}
```

> 💡 如果用户已经是该 team 成员（如之前已申请），返回已有的成员状态（可能是 `pending` 或 `approved`）。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效团队 ID、该团队不支持加入（非 teacher team 或无 join_code） |
| 401 | 未登录 |
| 403 | 非 student 角色 |
| 404 | 团队不存在 |

---

### POST /api/teams/join

学生凭班级码申请加入 teacher team（F2.5 主路径）。

**鉴权**：是（仅 `student` 角色）

**幂等**：是（已存在的成员关系直接返回）

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `code` | string | ✅ | 老师分享的 6 位班级码 |

**Response**

#### 200 OK

```json
{
  "status": "pending",
  "team_id": 2
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 班级码为空、班级码无效 |
| 401 | 未登录 |
| 403 | 非 student 角色 |

---

### POST /api/teams/:id/members/:uid/approve

老师审批学生加入 team（将 `pending` 改为 `approved`）。仅 team 创建者（owner）可操作（F2.5）。

**鉴权**：是（仅 `teacher` 角色 + 必须是该 team 的 owner）

**幂等**：✅ 是（重复审批不报错）

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 团队 ID |
| `uid` | int64 | 待审批的学生用户 ID |

**Response**

#### 200 OK

```json
{
  "ok": true
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效团队 ID 或用户 ID、成员关系不存在 |
| 401 | 未登录 |
| 403 | 非 teacher 角色、或非该 team 的 owner |

---

### GET /api/teams/:id/members

获取团队成员列表（含待审批成员）。仅 team 创建者（owner）可查看（F2.5）。

**鉴权**：是（`teacher` + team owner）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 团队 ID |

**Response**

#### 200 OK

```json
{
  "members": [
    {
      "TeamID": 2,
      "UserID": 1,
      "Role": "member",
      "Status": "approved",
      "JoinedAt": "2026-07-12T12:00:00Z"
    },
    {
      "TeamID": 2,
      "UserID": 3,
      "Role": "member",
      "Status": "pending",
      "JoinedAt": "2026-07-12T12:30:00Z"
    }
  ]
}
```

> 结果按 `status` → `joined_at` 排序：`approved` 在前，`pending` 在后。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效团队 ID |
| 401 | 未登录 |
| 403 | 仅团队创建者可查看成员 |

---

### GET /api/teams/:id/materials

获取指定 team 内的可见资料。自动根据当前用户身份过滤：
- teacher team owner 可见全部资料（含 `shared=false` 草稿）
- teacher team 中仅 `approved` 学生可见 `shared=true` 的资料；未加入或待审批用户返回空列表
- 私人 team owner 可见全部
- 公共库所有人可见

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 团队 ID |

**Response**

#### 200 OK

```json
{
  "materials": [
    {
      "ID": 10,
      "TeamID": 2,
      "Title": "一元二次方程讲义",
      "Subject": "数学",
      "Chapter": "第二章",
      "Tags": ["方程", "初中数学"],
      "Content": "# 一元二次方程\n\n...",
      "FileType": "md",
      "ParseStatus": "done",
      "Shared": true,
      "OwnerID": 2,
      "CreatedAt": "2026-07-12T11:30:00Z"
    }
  ],
  "can_write": true
}
```

`can_write` 由后端 repository 的 `CanWriteToTeam` 计算，仅用于控制写操作入口的展示；所有写请求仍会在服务端重新校验。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效团队 ID |
| 401 | 未登录 |

---

## 3. 资料 / Materials

> **权限安全**：所有资料查询均经 `VisibleMaterialsScope` 过滤——资料必须属于可见 team，且仅 owner 可读取自己的 teacher 草稿，其他成员还必须满足 `shared=true`。该谓词是用户可见资料的唯一真源，仅在 backend repository 层构建。

### GET /api/materials

获取当前用户可见的全部资料列表（按「可见 team 集合 + shared 过滤」），支持按 team 筛选和关键词搜索。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Query Parameters**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `team_id` | int | | 按 team 筛选（可选） |
| `q` | string | | 标题模糊搜索（ILIKE，可选） |
| `limit` | int | | 返回条数限制，默认 100 |

**Response**

#### 200 OK

```json
{
  "materials": [
    {
      "ID": 10,
      "TeamID": 2,
      "Title": "一元二次方程讲义",
      "Subject": "数学",
      "Chapter": "第二章",
      "Tags": ["方程", "初中数学"],
      "Content": "# 一元二次方程\n\n...",
      "FileType": "md",
      "ParseStatus": "done",
      "Shared": true,
      "OwnerID": 2,
      "CreatedAt": "2026-07-12T11:30:00Z"
    }
  ]
}
```

**示例请求**

```bash
# 获取全部可见资料
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/materials

# 按 team 筛选
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/materials?team_id=2"

# 标题搜索
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/materials?q=方程"
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |

---

### POST /api/materials

上传资料到指定 team。支持 JSON 和 multipart/form-data 两种提交方式。成功后**异步触发** Agent 解析（切分 → 嵌入 → 写 chunks → 更新 `parse_status`）。

**鉴权**：是（需对该 team 有写权限）

**幂等**：否

**写权限规则（repository 层强制）**

| Team 类型 | 允许上传的角色 |
|-----------|---------------|
| `private` | 仅 team owner（学生本人） |
| `teacher` | 仅 teacher team 的 owner 老师 |
| `public` | 仅 `super_admin` |

**Request Body — JSON**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `team_id` | int64 | ✅ | 归属团队 ID |
| `title` | string | ✅ | 资料标题 |
| `subject` | string | | 学科 |
| `chapter` | string | | 章节 |
| `tags` | string[] | | 标签列表 |
| `content` | string | | 正文（Markdown） |
| `file_type` | string | | 文件类型：`md` / `txt` / `pdf` / `pptx` / `docx` |

**Request Body — multipart/form-data**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `team_id` | int64 | ✅ | URL 路径参数 |
| `title` | string | ✅ | 表单字段 |
| `subject` | string | | 表单字段 |
| `chapter` | string | | 表单字段 |
| `tags` | string | | 逗号分隔，如 `"方程,初中数学"` |
| `content` | string | | 表单字段（可选，与 file 二选一即可） |
| `file` | file | | 上传文件（`.txt` / `.md` 自动读取正文） |

**Response**

#### 200 OK

```json
{
  "material": {
    "ID": 11,
    "TeamID": 2,
    "Title": "勾股定理讲义",
    "Subject": "数学",
    "Chapter": "第三章",
    "Tags": ["几何", "初中数学"],
    "Content": "# 勾股定理\n\n...",
    "FileType": "md",
    "ParseStatus": "pending",
    "Shared": false,
    "OwnerID": 2,
    "CreatedAt": "2026-07-12T14:00:00Z"
  }
}
```

> 📝 新创建的资料 `ParseStatus` 为 `pending`，后端异步触发 Agent 解析：`pending → parsing → done/failed`。每次 Agent 调用最多等待 120 秒，失败后最多进行 3 次指数退避重试；后端重启时会自动重新入队遗留的 `parsing`、派发持久化的 `pending`，运行期间每 5 秒补偿扫描漏派发任务。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 缺少 team_id 或 title |
| 401 | 未登录 |
| 403 | 无权向该团队上传资料 |

---

### GET /api/materials/:id

获取资料详情（含 `parse_status`）。访问前校验当前用户对该资料的可见性。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 资料 ID |

**Response**

#### 200 OK

```json
{
  "material": {
    "ID": 10,
    "TeamID": 2,
    "Title": "一元二次方程讲义",
    "Subject": "数学",
    "Chapter": "第二章",
    "Tags": ["方程", "初中数学"],
    "Content": "# 一元二次方程\n\n## 标准形式\n...",
    "FileType": "md",
    "ParseStatus": "done",
    "Shared": true,
    "OwnerID": 2,
    "CreatedAt": "2026-07-12T11:30:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效资料 ID |
| 401 | 未登录 |
| 404 | 资料不存在或当前用户不可见（统一响应，避免 ID 枚举） |

---

### PUT /api/materials/:id

更新资料信息（标题、正文、`shared` 可见性）。正文变更会触发重解析（R3 幂等：先删旧 chunks 再写新 chunks）。

**鉴权**：是（需对该 team 有写权限）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 资料 ID |

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `title` | string | | 新标题 |
| `content` | string | | 新正文（变更后触发重解析） |
| `shared` | bool | | 切换可见性——**仅 teacher team 的资料可设置**，私人/公共资料拒绝写入 |

**Response**

#### 200 OK

```json
{
  "material": {
    "ID": 10,
    "TeamID": 2,
    "Title": "一元二次方程讲义（修订版）",
    "Subject": "数学",
    "Chapter": "第二章",
    "Tags": ["方程", "初中数学"],
    "Content": "# 一元二次方程\n\n...（更新后内容）",
    "FileType": "md",
    "ParseStatus": "pending",
    "Shared": true,
    "OwnerID": 2,
    "CreatedAt": "2026-07-12T11:30:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效资料 ID、仅老师小组的资料可设置 shared 可见性 |
| 401 | 未登录 |
| 403 | 无权修改该资料 |
| 404 | 资料不存在 |

---

### POST /api/materials/:id/retry

将处于 `failed` 状态的解析任务重新入队。后端先复用 repository 的 team 写权限校验，再以条件更新执行 `failed → pending`；并发请求只有一个能成功，随后进入既有的超时与指数退避调度流程。

**鉴权**：是（需对该 team 有写权限）

**幂等**：条件幂等；非 `failed` 状态返回 `409 Conflict`

**Response**

#### 202 Accepted

```json
{
  "material": {
    "ID": 10,
    "ParseStatus": "pending",
    "ParseError": null
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效资料 ID 或资料不存在 |
| 401 | 未登录 |
| 403 | 无权重试该资料 |
| 409 | 资料当前不处于 `failed` 状态，或并发重试已被其他请求抢占 |

---

### DELETE /api/materials/:id

删除资料。数据库外键 `ON DELETE CASCADE` 级联删除关联的 `material_chunks`。

**鉴权**：是（需对该 team 有写权限）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 资料 ID |

**Response**

#### 200 OK

```json
{
  "ok": true
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效资料 ID |
| 401 | 未登录 |
| 403 | 无权删除该资料 |

---

## 4. 笔记 / Notes

> 阅读器场景：学生/老师只能在当前可见的资料上创建和读取自己的笔记。资料可见性统一由 repository `VisibleMaterialsScope` 校验。

### GET /api/materials/:id/notes

获取某资料下的所有笔记（当前用户自己的笔记）。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 资料 ID |

**Response**

#### 200 OK

```json
{
  "notes": [
    {
      "ID": 1,
      "UserID": 1,
      "MaterialID": 10,
      "Content": "注意判别式的三种情况：Δ>0、Δ=0、Δ<0",
      "Quote": "一元二次方程 $ax^2+bx+c=0$ 的判别式",
      "CreatedAt": "2026-07-12T15:00:00Z",
      "UpdatedAt": "2026-07-12T15:00:00Z"
    }
  ]
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效资料 ID |
| 401 | 未登录 |
| 404 | 资料不存在或当前用户不可见 |

---

### POST /api/materials/:id/notes

在资料上创建笔记（可附带选中文本引用）。

**鉴权**：是（所有角色）

**幂等**：否

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 资料 ID |

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | string | ✅ | 笔记内容 |
| `quote` | string | | 引用的原文片段 |

**Response**

#### 200 OK

```json
{
  "note": {
    "ID": 1,
    "UserID": 1,
    "MaterialID": 10,
    "Content": "注意判别式的三种情况",
    "Quote": "一元二次方程 $ax^2+bx+c=0$",
    "CreatedAt": "2026-07-12T15:00:00Z",
    "UpdatedAt": "2026-07-12T15:00:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 笔记内容必填、无效资料 ID |
| 401 | 未登录 |
| 404 | 资料不存在或当前用户不可见 |

---

### PUT /api/notes/:id

更新笔记内容。仅笔记作者可更新。

**鉴权**：是（所有角色，但仅限笔记所有者）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 笔记 ID |

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | string | ✅ | 新笔记内容 |

**Response**

#### 200 OK

```json
{
  "ok": true
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 笔记内容必填、无效笔记 ID、非笔记作者 |
| 401 | 未登录 |

---

### DELETE /api/notes/:id

删除笔记。仅笔记作者可删除。

**鉴权**：是（所有角色，但仅限笔记所有者）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 笔记 ID |

**Response**

#### 200 OK

```json
{
  "ok": true
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效笔记 ID、非笔记作者 |
| 401 | 未登录 |

---

## 5. 学习记录 / Learning

### POST /api/learning/records

创建学习记录（学习时长、进度、测验得分）。按 `user_id` 行级隔离。

**鉴权**：是（所有角色）

**幂等**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `material_id` | int64 | | 关联资料 ID（可选） |
| `duration_s` | int | ✅ | 学习时长（秒） |
| `progress` | float64 | ✅ | 完成进度（0–100） |
| `score` | float64 | | 测验得分（可选） |

**Response**

#### 200 OK

```json
{
  "record": {
    "ID": 1,
    "UserID": 1,
    "MaterialID": 10,
    "DurationS": 3600,
    "Progress": 75.5,
    "Score": 92,
    "CreatedAt": "2026-07-12T16:00:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 请求格式错误 |
| 401 | 未登录 |

---

### GET /api/learning/records

获取当前用户的学习记录列表。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Response**

#### 200 OK

```json
{
  "records": [
    {
      "ID": 1,
      "UserID": 1,
      "MaterialID": 10,
      "DurationS": 3600,
      "Progress": 75.5,
      "Score": 92,
      "CreatedAt": "2026-07-12T16:00:00Z"
    }
  ]
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |

---

### GET /api/learning/progress

获取当前用户的学习进度聚合摘要。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Response**

#### 200 OK

```json
{
  "summary": {
    "total_duration_s": 14400,
    "avg_progress": 68.3,
    "quiz_count": 15,
    "quiz_correct": 11,
    "quiz_accuracy": 0.73,
    "daily": [
      {
        "date": "2026-07-11",
        "duration_s": 5400,
        "avg_progress": 60
      },
      {
        "date": "2026-07-12",
        "duration_s": 9000,
        "avg_progress": 75
      }
    ]
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `total_duration_s` | int | 总学习时长（秒） |
| `avg_progress` | float64 | 平均完成度 |
| `quiz_count` | int | 总答题数 |
| `quiz_correct` | int | 答对数 |
| `quiz_accuracy` | float64 | 正确率（0–1） |
| `daily` | object[] | 每日统计 `[{date, duration_s, avg_progress}]` |

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |

---

## 6. Agent / AI

> **安全边界**：后端 repository 先应用统一资料可见性谓词并完成 pgvector top-k，再把已授权 chunks 交给 Agent。`material_id` 也必须通过同一 repository 校验；不可见与不存在均返回 404。Agent 不提供检索权限面（system-design §7.4）。

### SSE 流式协议

`POST /api/agent/chat` 使用 Server-Sent Events（SSE）流式返回。事件格式：

```
data: {"type":"citations","items":[{team_id,material_id,chapter,chunk_idx,snippet}]}

data: {"type":"token","text":"关于"}

data: {"type":"token","text":"这个"}

data: {"type":"token","text":"问题"}
...

data: {"type":"done","citations":[...],"prompt_tokens":150,"completion_tokens":80}

data: {"type":"end"}
```

| Event Type | 说明 |
|------------|------|
| `citations` | 检索引用的资料片段列表（最先发送） |
| `token` | 逐字输出文本（汉字按 3 字为粒度、英文按词/空格分割） |
| `done` | 回答结束，附带引用列表和 token 用量 |
| `error` | 错误信息 |
| `end` | 后端收尾完成（handler 层附加，表示消息已落库） |

> ⚠️ 为防止 token 通过 URL 泄漏（R4），前端应使用 `fetch` + `ReadableStream` 实现 SSE，在 Header 中携带 `Authorization: Bearer {token}`，**不要使用 `EventSource`**。

---

### POST /api/agent/chat

AI 对话（SSE 流式）。后端自动：repository 应用可见性谓词并完成 pgvector top-k → 新建或复用会话 → 加载历史消息 → 将已授权 chunks 交给 Agent 流式生成 → 转发 SSE 事件 → 落 assistant 消息 + token 用量。

**鉴权**：是（所有角色）

**幂等**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `question` | string | ✅ | 用户问题 |
| `session_id` | string | | 会话 ID。空则自动新建（标题取 question 前 40 字） |
| `material_id` | int64 | | 关联资料 ID，限定在此资料上下文中对话 |

**Response**：SSE 流式（`Content-Type: text/event-stream`），见上方 [SSE 流式协议](#sse-流式协议)。

**示例请求**

```bash
curl -N \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"一元二次方程的求根公式是什么？"}' \
  http://localhost:8080/api/agent/chat
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 缺少 question |
| 401 | 未登录 |
| 502 | Agent 服务不可用 |

---

### GET /api/agent/sessions

获取当前用户的对话会话列表。

**鉴权**：是（所有角色）

**幂等**：✅ 是

**Response**

#### 200 OK

```json
{
  "sessions": [
    {
      "ID": "550e8400-e29b-41d4-a716-446655440000",
      "UserID": 1,
      "Title": "一元二次方程的求根公式是什么？",
      "CreatedAt": "2026-07-12T16:30:00Z"
    }
  ]
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |

---

### GET /api/agent/sessions/:id

获取指定会话的完整消息历史。

**鉴权**：是（所有角色，会话归属用户）

**幂等**：✅ 是

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | string (UUID) | 会话 ID |

**Response**

#### 200 OK

```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "messages": [
    {
      "ID": 1,
      "SessionID": "550e8400-e29b-41d4-a716-446655440000",
      "Role": "user",
      "Content": "一元二次方程的求根公式是什么？",
      "Citations": null,
      "CreatedAt": "2026-07-12T16:30:00Z"
    },
    {
      "ID": 2,
      "SessionID": "550e8400-e29b-41d4-a716-446655440000",
      "Role": "assistant",
      "Content": "一元二次方程 $ax^2+bx+c=0$ 的求根公式为...",
      "Citations": [{"team_id":2,"material_id":10,"chapter":"第二章"}],
      "CreatedAt": "2026-07-12T16:30:05Z"
    }
  ]
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 401 | 未登录 |
| 404 | 会话不存在 |

---

### POST /api/agent/plan

生成学习计划。Agent（Planner）根据目标与期限生成结构化计划，自动落库到 `study_plans` 表（F7）。

**鉴权**：是（所有角色）

**幂等**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `goal` | string | ✅ | 学习目标（如"两周后考教资"） |
| `deadline` | string | | 截止日期（`2006-01-02` 格式，如 `"2026-07-26"`） |
| `title` | string | | 计划标题（默认取 Agent 返回的 title） |

**Response**

#### 200 OK

```json
{
  "plan": {
    "ID": 1,
    "UserID": 1,
    "Title": "学习计划：两周后考教资",
    "Goal": "两周后考教资",
    "Deadline": "2026-07-26T00:00:00Z",
    "Items": [
      {"date": "D1", "task": "预习核心概念", "done": false},
      {"date": "D2", "task": "精读资料并做标注", "done": false},
      {"date": "D3", "task": "完成配套例题", "done": false},
      {"date": "D4", "task": "整理笔记与错题", "done": false},
      {"date": "D5", "task": "自测回顾", "done": false}
    ],
    "CreatedAt": "2026-07-12T17:00:00Z"
  }
}
```

> 💡 Items 为 JSONB 数组 `[{date: string, task: string, done: bool}]`，前端可据此渲染计划列表和打卡。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 缺少 goal |
| 401 | 未登录 |
| 502 | Agent 规划失败 |

---

### POST /api/agent/quiz

生成智能测评题目。Agent（Evaluator）根据主题与资料出题，自动落库到 `exercises` 表（F8）。

**鉴权**：是（所有角色）

**幂等**：否

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `topic` | string | ✅ | 测评主题（如"一元二次方程"） |
| `material_id` | int64 | | 限定在某资料内出题（检索仅在该资料的 chunks 内） |
| `count` | int | | 出题数量，默认 5 |

**Response**

#### 200 OK

```json
{
  "exercises": [
    {
      "ID": 1,
      "MaterialID": 10,
      "Question": "关于「一元二次方程」，下列说法正确的是？\n（参考：一元二次方程的标准形式为 $ax^2+bx+c=0(a≠0)$...）",
      "Options": ["A选项内容", "B选项内容", "C选项内容", "D选项内容"],
      "Difficulty": "medium",
      "CreatedAt": "2026-07-12T17:10:00Z"
    }
  ]
}
```

> `Options` 以数组返回；正确答案只保存在后端，生成响应不包含 `AnswerKey`。

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 请求格式错误 |
| 401 | 未登录 |
| 502 | Agent 出题失败 |

---

### POST /api/agent/quiz/:id/answer

提交测评答案并批改。批改为**本地判定**（大小写不敏感比较 `A/B/C/D` 选项标识与 `answer_key`），答对得满分 100，答错得 0 分。只有题目的生成用户可作答；其他用户统一获得 404。作答记录落 `quiz_attempts` 表（F8）。

**鉴权**：是（所有角色）

**幂等**：否（每次提交创建新 attempt 记录）

**Path Parameters**

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 题目 ID |

**Request Body（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `choice` | string | ✅ | 作答选择（如 `"A"`，大小写不敏感） |

**Response**

#### 200 OK

```json
{
  "is_correct": true,
  "correct_key": "A",
  "attempt": {
    "ID": 1,
    "UserID": 1,
    "ExerciseID": 1,
    "Choice": "A",
    "IsCorrect": true,
    "Score": 100,
    "CreatedAt": "2026-07-12T17:15:00Z"
  }
}
```

#### 错误码

| HTTP | 说明 |
|------|------|
| 400 | 无效题目 ID、缺少 choice |
| 401 | 未登录 |
| 404 | 题目不存在 |

---

## 附录 A：错误码速查 / Error Code Reference

| HTTP 状态码 | 常量 | 常见原因 |
|-------------|------|---------|
| 400 | Bad Request | 参数校验失败、必填字段缺失、格式错误 |
| 401 | Unauthorized | 未登录、token 缺失或失效、密码错误 |
| 403 | Forbidden | 角色不足（RBAC）、无权操作该资源、非 team owner |
| 404 | Not Found | 资源不存在（用户/资料/会话/题目） |
| 502 | Bad Gateway | Agent 服务不可达或内部错误 |

---

## 附录 B：资料解析状态机 / Parse Status

```
pending ──→ parsing ──→ done
  ▲             │
  │             └──→ failed（含 parse_error 详情）
  └──── 后端重启恢复遗留 parsing，或所有者显式重试 failed
```

| 状态 | 说明 | 前端表现 |
|------|------|---------|
| `pending` | 待解析，任务已入队 | 显示「解析中…」加载态 |
| `parsing` | Agent 正在处理 | 显示「解析中…」加载态 |
| `done` | 解析完成，chunks 已入库 | 可正常 RAG 检索 |
| `failed` | 3 次尝试均失败，`parse_error` 含最后错误；作为可观测死信状态并记录错误日志 | 对有写权限的用户显示「重试解析」 |

解析调度器以 `materials.parse_status` 作为数据库持久化队列，并在每次重新入队时递增 `parse_generation`。repository 条件更新抢占 `pending → parsing`；worker 只有代次匹配时才能写最终状态。Agent 在短事务内获取 material 级 advisory lock，并仅在代次匹配且状态仍为 `parsing` 时替换 chunks；数据库唯一索引 `(material_id, chunk_idx)` 继续防止重复片段。

若配置 `PARSE_ALERT_WEBHOOK_URL`，任务重试耗尽后会以 5 秒超时发送 `material_parse_failed` JSON 事件（包含 `material_id`、截断至 512 字符的错误与 UTC 时间）；告警失败只记录结构化错误，不会覆盖已经持久化的 `failed` 状态。

---

## 附录 C：请求/响应类型汇总 / Type Reference

### Go 模型 → JSON 字段映射

GORM 模型未显式设置 `json` tag，序列化时使用结构体字段名（PascalCase）。以下为各资源的关键字段：

**User**
```
ID, Email, DisplayName, Role, Subscription, CreatedAt
```

**Team**
```
ID, Name, Type, JoinCode, OwnerID, CreatedAt
```

**TeamMember**
```
TeamID, UserID, Role, Status, JoinedAt
```

**Material**
```
ID, TeamID, Title, Subject, Chapter, Tags, Content, FileType,
StorageKey, ParseStatus, ParseGeneration, ParseError, Shared, OwnerID, CreatedAt
```

**MaterialNote**
```
ID, UserID, MaterialID, Content, Quote, CreatedAt, UpdatedAt
```

**LearningRecord**
```
ID, UserID, MaterialID, DurationS, Progress, Score, CreatedAt
```

**AgentSession**
```
ID, UserID, Title, CreatedAt
```

**AgentMessage**
```
ID, SessionID, Role, Content, Citations, CreatedAt
```

**Exercise（内部持久化模型）**
```
ID, UserID, MaterialID, Question, Options, AnswerKey, Difficulty, CreatedAt
```

> `POST /api/agent/quiz` 使用不含 `UserID` / `AnswerKey` 的专用公开 DTO，不会序列化上述内部字段。

**QuizAttempt**
```
ID, UserID, ExerciseID, Choice, IsCorrect, Score, CreatedAt
```

**StudyPlan**
```
ID, UserID, Title, Goal, Deadline, Items, CreatedAt
```
