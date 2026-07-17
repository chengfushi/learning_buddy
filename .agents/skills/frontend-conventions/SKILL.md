---
name: frontend-conventions
description: React 前端（learning_buddy/frontend）开发规范——TypeScript 严格模式、组件结构、API 契约校验、SSE 模式、样式约定、工具链。编写或评审 frontend/ 代码时自动遵循。
---

# Frontend 开发规范（React / TypeScript / Vite）

适用目录：`frontend/`。技术栈：React 18 + TypeScript 5 + Vite 5 + React Query 3 + Zod。

## 0. 架构原则

- **前端不拼权限谓词**（铁律）：只消费后端下发的已过滤数据，不自行构建可见性 SQL/条件。
- **关注点分离**：API 调用（`api.ts`）→ 状态管理（`auth.tsx` + React Query）→ 页面组件（`pages/*.tsx`）。
- **页面就近 co-locate**：一个页面的子组件、样式、hooks 放在同目录下。

```
frontend/src/
├── api.ts          # 后端 REST 契约 + Zod 校验
├── auth.tsx        # AuthContext + AuthProvider
├── App.tsx         # 路由 / 视图切换
├── main.tsx        # 入口
├── styles.css      # 全局样式
└── pages/          # 页面组件
    ├── Login.tsx
    ├── Library.tsx
    ├── Teams.tsx
    ├── Companion.tsx
    ├── Learning.tsx
    └── Reader.tsx
```

## 1. TypeScript

### 1.1 严格模式

`tsconfig.json` 配置：
```json
{
  "compilerOptions": {
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  }
}
```

- **禁止 `any`**（`@typescript-eslint/no-explicit-any: error`）。
- 所有函数参数和返回值必须有类型标注。
- 使用 `interface` 定义数据结构，不用 `type`（保持代码库一致）。

### 1.2 API 契约

```ts
// ✅ 正确：Zod 校验后端响应
import { z } from "zod";

const TeamSchema = z.object({
  ID: z.number(),
  Name: z.string(),
  Type: z.enum(["private", "teacher", "public"]),
  JoinCode: z.string().nullable(),
});

type Team = z.infer<typeof TeamSchema>;

// ✅ 正确：接口定义与后端模型字段一一对应（后端 GORM 按 Go 字段名序列化，首字母大写）
export interface Material {
  ID: number;
  TeamID: number;
  Title: string;
  Shared: boolean;
  ParseStatus: string;
  // ...
}
```

- 每个 API 响应用 Zod schema 校验（防止后端变更破坏前端）。
- 接口字段名与后端 Go 结构体字段名保持一致（大写开头）。
- 可空字段显式标注 `| null`。

### 1.3 路径别名

```ts
// tsconfig.json paths: { "@/*": ["src/*"] }
import { useAuth } from "@/auth";
```

## 2. React 组件

### 2.1 组件结构

```tsx
// ✅ 函数组件 + 显式类型
export default function Library() {
  const [materials, setMaterials] = useState<Material[]>([]);

  useEffect(() => {
    api.listMaterials().then(setMaterials).catch(console.error);
  }, []);

  return <div>...</div>;
}
```

- 函数组件 + Hooks（不使用 class 组件）。
- Props 类型定义在组件上方或使用内联 `{ ... }: { prop: Type }`。
- 全局状态用 React Context（`auth.tsx`），页面状态用 `useState`。
- 服务器状态用 `react-query`（缓存/同步后端数据）。

### 2.2 错误边界

```tsx
// ✅ 全局错误兜底
class ErrorBoundary extends React.Component<{children: ReactNode}, {hasError: boolean}> {
  // ...
}
```

- 每个数据获取的页面应处理 loading / error / empty 三种状态。
- 全局顶层应有 `ErrorBoundary` 兜底。

### 2.3 条件渲染

```tsx
// ✅ 先处理 loading / error / empty，再渲染主逻辑
if (!ready) return <div className="loading">加载中…</div>;
if (!user) return <Login />;
if (materials.length === 0) return <div className="empty">暂无资料</div>;

return <MaterialList items={materials} />;
```

- Loading 态：显示加载指示器，不能白屏。
- Error 态：显示错误信息 + 重试按钮。
- Empty 态：显示引导文案。

## 3. SSE 与流式通信

```tsx
// ✅ 正确：fetch + ReadableStream（可带 Authorization Header）
const response = await fetch(`${apiBase}/agent/chat`, {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    Authorization: `Bearer ${getToken()}`,
  },
  body: JSON.stringify({ question, visible_team_ids: [...], history }),
});

const reader = response.body.getReader();
// 逐 chunk 解析 SSE

// ❌ 错误：EventSource（不能带 Authorization Header，token 进 URL 导致泄漏）
const es = new EventSource(`${apiBase}/agent/chat?token=${getToken()}`);
```

- **必须用 `fetch` + `ReadableStream`**，不使用 `EventSource`（engineering-standards R4：SSE 鉴权 token 不进入 URL）。
- 流式内容实时渲染：逐 token 追加到 UI。

## 4. 样式（CSS）

```css
/* 全局样式在 styles.css */
/* 使用 BEM-like 命名约定 */
.app { max-width: 960px; margin: 0 auto; }
.topbar { display: flex; align-items: center; ... }
.nav-on { color: #667eea; font-weight: 600; }
```

- 页面级 class 用功能命名（`.library`, `.teams`, `.companion`）。
- 状态 class 用 `-on` / `-off` 后缀（`.nav-on`）。
- 颜色值直接使用 hex（项目使用紫色主题 `#667eea`）。

## 5. 状态管理

- **服务端数据**：`react-query`。
- **跨组件共享**：Context（如 `AuthContext`）。
- **页面内局部**：`useState`。
- API 调用的 loading / error / data 三态在组件中处理。

```tsx
// ✅ React Query 模式
const { data: teams, isLoading, error } = useQuery("teams", api.listTeams);
if (isLoading) return <Loading />;
if (error) return <Error message={error.message} />;
```

## 6. 工具链

| 工具 | 命令 | 用途 |
|------|------|------|
| `tsc -b` | 类型检查 | CI 阻断 |
| `prettier` | `npm run format` | 格式化（semi: true, singleQuote: false, printWidth: 100） |
| `eslint` | `npm run lint` | Lint（recommended + typescript + react-hooks） |
| `vite build` | `npm run build` | 生产构建（CI 必跑） |

### 6.1 提交前检查

安装 hooks 后（`git config core.hooksPath .githooks`），每次提交自动执行：
```bash
npm run format    # prettier --write
npm run lint      # eslint --max-warnings 0
```

## 7. 禁止清单

- ❌ `any` 类型（eslint error）
- ❌ `EventSource` 传 SSE（token 进 URL → 安全泄漏 R4）
- ❌ 在前端拼权限谓词或可见 team 过滤（铁律）
- ❌ 组件中直接操作 DOM（用 React state）
- ❌ 硬编码 API 地址（用 `VITE_API_BASE` 环境变量）
- ❌ 未处理的 Promise rejection（`.catch()` 或 try/catch）
- ❌ API 响应未做 Zod 校验
- ❌ 提交 `node_modules/` 或构建产物（`.gitignore` 已覆盖）
