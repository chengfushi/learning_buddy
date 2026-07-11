#!/usr/bin/env bash
# learning_buddy 提交前钩子：对每个存在的服务做代码格式化与静态检查。
# 安装：git config core.hooksPath .githooks
set -uo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; NC='\033[0m'
fail=0

have() { command -v "$1" >/dev/null 2>&1; }

backend() {
  [ -d "$ROOT/backend" ] || return 0
  echo -e "${GREEN}== backend (Go) ==${NC}"
  if ! have go; then echo -e "${RED}✗ go 未安装${NC}"; fail=1; return; fi
  ( cd "$ROOT/backend" && echo -e "${YELLOW}▶ gofmt -w .${NC}" && gofmt -w . ) || fail=1
  ( cd "$ROOT/backend" && echo -e "${YELLOW}▶ go vet ./...${NC}" && go vet ./... ) || fail=1
  if have golangci-lint; then
    ( cd "$ROOT/backend" && echo -e "${YELLOW}▶ golangci-lint run${NC}" && golangci-lint run ) || fail=1
  else
    echo -e "${YELLOW}！ golangci-lint 未安装，跳过（建议安装以启用门禁）${NC}"
  fi
}

frontend() {
  [ -d "$ROOT/frontend" ] || return 0
  echo -e "${GREEN}== frontend (React/Vite) ==${NC}"
  if ! have npm; then echo -e "${RED}✗ npm 未安装${NC}"; fail=1; return; fi
  ( cd "$ROOT/frontend" && echo -e "${YELLOW}▶ npm run format${NC}" && npm run format ) || fail=1
  ( cd "$ROOT/frontend" && echo -e "${YELLOW}▶ npm run lint${NC}" && npm run lint ) || fail=1
  if [ "${LB_VITE_BUILD:-0}" = "1" ]; then
    ( cd "$ROOT/frontend" && echo -e "${YELLOW}▶ npm run build (vite)${NC}" && npm run build ) || fail=1
  else
    echo -e "${YELLOW}! 设 LB_VITE_BUILD=1 可在本地跑 vite build 门禁（CI 默认开启）${NC}"
  fi
}

agent() {
  [ -d "$ROOT/agent" ] || return 0
  echo -e "${GREEN}== agent (Python) ==${NC}"
  if ! have ruff; then echo -e "${RED}✗ ruff 未安装（pip install ruff）${NC}"; fail=1; return; fi
  ( cd "$ROOT/agent" && echo -e "${YELLOW}▶ ruff format .${NC}" && ruff format . ) || fail=1
  ( cd "$ROOT/agent" && echo -e "${YELLOW}▶ ruff check .${NC}" && ruff check . ) || fail=1
}

backend
frontend
agent

if [ "$fail" -ne 0 ]; then
  echo -e "${RED}✗ 提交被阻断：请修复上述检查后再提交。${NC}"
  exit 1
fi
echo -e "${GREEN}✓ 所有检查通过，可以继续提交。${NC}"
