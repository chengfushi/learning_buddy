# Frontend 服务（React + TypeScript + Vite）

> 智能学伴系统前端——「自主学习」与「AI 辅助学习」两大模式，面向学生、老师、超级管理员三类用户。

[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react)](https://react.dev/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.5-3178C6?logo=typescript)](https://www.typescriptlang.org/)
[![Vite](https://img.shields.io/badge/Vite-5-646CFF?logo=vite)](https://vitejs.dev/)

---

## 架构概览 / Architecture

```
用户/浏览器
    │ HTTPS / REST + SSE
    ▼
React 18 SPA (Vite + TypeScript)
    ├── 路由：知识库 / 团队 / AI 学伴 / 学习中心
    ├── 状态：Zustand（全局）+ React Query（服务端缓存）
    ├── 鉴权：短时 JWT Bearer Token（仅内存）+ httpOnly refresh Cookie
    └── AI 流式：fetch + ReadableStream（SSE，带 Authorization Header）
```

---

## 技术栈 / Tech Stack

| 组件 | 版本 / 库 | 说明 |
|------|-----------|------|
| 语言 | TypeScript 5.5（strict） | 全量类型，禁止 `any` |
| 框架 | React 18 | SPA |
| 构建 | Vite 5 | 极速 HMR |
| 状态管理 | React Query v3 | 服务端状态缓存与自动刷新 |
| 校验 | Zod v3 | 运行时类型校验 |
| 样式 | CSS（native） | 无第三方组件库依赖 |
| 质量 | ESLint 8 + Prettier 3 | 提交前自动格式化 |

---

## 快速开始 / Quick Start

### 前置依赖

- Node.js 18+
- 后端服务运行中（默认 `http://localhost:8080`）

### 安装与运行

```bash
cd frontend

# 1. 配置环境变量
cp .env.example .env
# 编辑 .env：VITE_API_BASE 指向后端地址

# 2. 安装依赖
npm install

# 3. 启动开发服务器
npm run dev
# 默认 http://localhost:5173

# 4. 构建生产版本
npm run build
# 输出到 dist/
```

---

## 目录结构 / Project Structure

```
frontend/
├── index.html                # HTML 入口
├── package.json              # 依赖 + 脚本
├── tsconfig.json             # TypeScript 配置（strict）
├── vite.config.ts            # Vite 配置（含 API 代理）
├── .eslintrc.cjs             # ESLint 配置
├── .prettierrc               # Prettier 配置
├── .env.example              # 环境变量模板
├── dist/                     # 生产构建输出
└── src/
    ├── main.tsx              # ReactDOM 入口
    ├── App.tsx               # 根组件：路由 + 顶部导航 + 页面切换
    ├── auth.tsx              # 鉴权上下文（useAuth Hook）
    ├── api.ts                # 后端 API 封装（REST + SSE 流式）
    ├── styles.css            # 全局样式 + 组件样式
    └── pages/
        ├── Login.tsx         # 登录 / 注册页（含角色选择）
        ├── Library.tsx       # 知识库（资料列表 / 搜索 / 上传 / shared 开关）
        ├── Teams.tsx         # 团队管理（创建 / 加入 / 审批成员）
        ├── Reader.tsx        # 阅读器（资料正文 + 笔记 + 一键提问）
        ├── Companion.tsx     # AI 学伴（对话列表 / 流式对话 / 引用展示）
        └── Learning.tsx      # 学习中心（进度统计 / 学习记录 / 计划 / 测评）
```

---

## 页面与功能 / Pages & Features

### `Login.tsx` — 登录 / 注册

- 邮箱 + 密码登录
- 注册支持选择角色（student / teacher）
- 注册成功后自动登录

### `Library.tsx` — 知识库

- 按 team 筛选资料列表（支持关键词搜索）
- 上传资料（粘贴正文 / Markdown，指定归属 team）
- 老师角色可切换资料 `shared` 可见性
- 点击资料进入阅读器

### `Teams.tsx` — 团队管理

- 查看所有可见 team（私人 + 已加入 + 公共库）
- 老师：创建学习小组（生成 `join_code`）、审批待加入学生
- 学生：凭 `join_code` 申请加入老师 team
- 查看 team 成员列表与审批状态

### `Reader.tsx` — 阅读器

- 资料正文阅读
- 创建 / 查看笔记（可引用选中文本）
- 一键「问学伴」——跳转到 AI 学伴页面并关联当前资料

### `Companion.tsx` — AI 学伴

- 历史会话列表（可切换/新建）
- **流式对话**：SSE 逐字输出 + 显示引用来源（team / 资料 / 章节）
- 支持普通答疑、学习计划生成、智能测评
- 对话内容含引用链接（可定位到原文）

### `Learning.tsx` — 学习中心

- 学习进度总览（总时长、平均完成度、测验正确率）
- 每日学习时长趋势
- 学习记录列表

---

## 环境变量 / Environment Variables

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `VITE_API_BASE` | 后端 API 基地址 | `http://localhost:8080` |

---

## API 封装说明 / API Client

`src/api.ts` 提供完整的类型安全 API 封装，与后端 REST 端点一一对应：

| 模块 | 主要方法 |
|------|---------|
| 账号 | `api.register()` / `api.login()` / `api.me()` |
| 团队 | `api.listTeams()` / `api.createTeam()` / `api.joinTeam()` / `api.joinByCode()` / `api.approveMember()` |
| 资料 | `api.listMaterials()` / `api.createMaterial()` / `api.getMaterial()` / `api.updateMaterial()` / `api.deleteMaterial()` |
| 笔记 | `api.listNotes()` / `api.createNote()` |
| 学习 | `api.createLearningRecord()` / `api.listLearningRecords()` / `api.getProgress()` |
| Agent | `api.createPlan()` / `api.createQuiz()` / `api.answerQuiz()` / `api.listSessions()` |
| SSE | `api.chatStream()`（`fetch` + `ReadableStream`，**不用 `EventSource`**） |

> **安全设计**：SSE 使用 `fetch` + `ReadableStream` 实现，可带 `Authorization` Header，避免 `EventSource` 将 token 暴露在 URL 中（R4）。

### 类型定义

所有类型与后端 GORM 模型字段严格对齐（Go 字段名序列化后为 PascalCase，如 `ID`、`TeamID`、`ParseStatus`）：

```typescript
User | Team | Material | MaterialNote | TeamMember
LearningRecord | AgentSession | StudyPlan | Exercise
Citation | ChatResult | ProgressSummary | DailyProgress
```

---

## 常用命令 / Commands

```bash
# 开发（HMR）
npm run dev

# 生产构建
npm run build

# 预览生产构建
npm run preview

# 静态检查
npm run lint

# 自动格式化
npm run format
```

---

## 设计要点 / Design Notes

1. **类型安全**：TypeScript `strict: true`，禁用 `any`。所有 API 请求/响应严格类型化，Zod 校验后端响应。
2. **SSE 鉴权**：使用 `fetch` + `ReadableStream` 实现流式对话（`streamPost()`），可在 Header 中携带 JWT token，避免 `EventSource` 的 URL 参数泄漏问题（R4）。
3. **鉴权管理**：`auth.tsx` 提供 `useAuth()` Hook，负责登录态维护、401 自动刷新、内存 token 与角色感知；refresh token 不可被 JavaScript 读取。
4. **错误处理**：`ApiError` 类统一封装 HTTP 错误，前端组件可基于 `status` 码差异化提示。
5. **零组件库**：纯 CSS 实现，无第三方 UI 组件依赖，便于团队自主掌控样式。
