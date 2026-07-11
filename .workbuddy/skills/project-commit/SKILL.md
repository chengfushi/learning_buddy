---
name: project-commit
description: 智能学伴系统（learning_buddy）的统一提交流程。当用户要提交代码、写 commit、或说“提交/commit”时调用。提交前对每个存在的服务做代码格式化与检查（backend: gofmt+golangci-lint；frontend: prettier+eslint，vite build 在 CI；agent: ruff format+check），自动修复可安全格式化的内容，最后用 Conventional Commits 生成提交信息。
license: MIT
disable: false
---

# 项目提交流程（learning_buddy）

本技能统一本仓库的「提交前检查 + 提交」动作，保证合入主干的代码一定经过格式化与静态检查。

## 何时使用
- 用户说「提交 / commit / 帮我提交」。
- 用户改完代码准备合入，需要先跑 `gofmt` / `vite` / `ruff` 格式化与检查。

## 前置：安装 Git 钩子（一次性）

仓库自带 `.githooks/pre-commit`，提交前自动跑全部检查。安装：
```bash
git config core.hooksPath .githooks
```
（未安装也能手动跑下方命令；装了则 `git commit` 自动触发。）

## 提交前检查（按服务存在与否执行）

> 仅对仓库中**实际存在**的目录执行对应步骤；某服务不存在则跳过。

### backend（Go）
```bash
cd backend
gofmt -w .                      # 自动格式化
go vet ./...
golangci-lint run               # 若失败则按提示修复（非安全项可 golangci-lint run --fix）
```

### frontend（React / Vite）
```bash
cd frontend
npm run format                  # prettier --write
npm run lint                    # eslint
# vite 生产构建作为完整类型/打包门禁，CI 中执行；本地可选：
npm run build                   # vite build
```

### agent（Python）
```bash
cd agent
ruff format .                  # 自动格式化
ruff check .                   # 若失败可 ruff check --fix .
mypy --strict .                # 类型检查（可选，按团队节奏开启阻断）
```

任何一步返回非零，**停止提交**，修复后再继续。

## 提交

1. `git status` / `git diff --stat` 确认改动范围。
2. `git add` 仅暂存本次相关文件（避免 `git add -A` 误带密钥/产物）。
3. 用 Conventional Commits 写信息，scope 标注服务：
   ```
   feat(backend): 资料 repository 集中化可见 team 谓词
   fix(agent): Retriever 超时降级为无 RAG
   docs: 补充数据库文档
   ```
4. `git commit -m "..."`（钩子会自动再跑一遍检查；若钩子报错，修完 `git commit --amend` 或补 commit）。
5. 如需推远：`git push`（默认不开 `--force`）。

## 注意
- 绝不提交 `.env`、密钥、`node_modules/`、编译产物（见根 `.gitignore`）。
- 权限/计费相关改动必须已带测试（见 `project-conventions` 铁律）。
- 提交信息用中文或英文均可，但必须遵循 Conventional Commits 前缀。
