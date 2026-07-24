# learning_buddy 常用命令

COMPOSE_DEV = docker compose -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: infra dev dev-down dev-restart dev-rebuild lint format migrate provision-parser reindex-rag-v2 activate-rag-v2 rollback-rag-v2

## 启动宿主机独立基础设施容器（不属于本项目 Compose）
infra:
	docker start local-pg17-vector redis minio

## 启动开发环境（源码热更新）
dev:
	$(COMPOSE_DEV) up -d

## 停止开发环境
dev-down:
	$(COMPOSE_DEV) down

## 重启开发环境（代码修改通常无需执行，热更新会自动生效）
dev-restart:
	$(COMPOSE_DEV) restart

## Compose、Dockerfile 或依赖修改后重新构建
dev-rebuild:
	$(COMPOSE_DEV) up -d --build --force-recreate

## 格式化（自动修复可安全格式化的内容）
format:
	cd backend && gofmt -w .
	cd frontend && npm run format
	cd agent && uv run ruff format .

## 静态检查
lint:
	cd backend && golangci-lint run
	cd frontend && npm run lint
	cd agent && uv run ruff check .

## 按文件名顺序执行全部数据库迁移；调用示例：DB_DSN=postgres://... make migrate
migrate:
	@test -n "$$DB_DSN" || (echo "DB_DSN is required" >&2; exit 1)
	@set -e; for migration in backend/migrations/*.sql; do \
		echo "applying $$migration"; \
		psql "$$DB_DSN" -v ON_ERROR_STOP=1 -f "$$migration"; \
	done

## 用管理员 DSN 创建/更新 Agent Parser 最小权限账号
provision-parser:
	@test -n "$$DB_DSN" || (echo "DB_DSN is required" >&2; exit 1)
	@test -n "$$PARSER_DB_PASSWORD" || (echo "PARSER_DB_PASSWORD is required" >&2; exit 1)
	@psql "$$DB_DSN" -v ON_ERROR_STOP=1 --single-transaction -f backend/scripts/provision_parser.sql

reindex-rag-v2:
	@test -n "$$DB_DSN" || (echo "DB_DSN is required" >&2; exit 1)
	@psql "$$DB_DSN" -v ON_ERROR_STOP=1 -f backend/scripts/reindex_rag_v2.sql

activate-rag-v2:
	@test -n "$$DB_DSN" || (echo "DB_DSN is required" >&2; exit 1)
	@case "$$RAG_ROLLOUT_PERCENTAGE" in 10|50|100) ;; \
		*) echo "RAG_ROLLOUT_PERCENTAGE must be 10, 50, or 100" >&2; exit 1 ;; esac
	@psql "$$DB_DSN" -v ON_ERROR_STOP=1 \
		-v rollout_percentage="$$RAG_ROLLOUT_PERCENTAGE" \
		-f backend/scripts/activate_rag_v2.sql

rollback-rag-v2:
	@test -n "$$DB_DSN" || (echo "DB_DSN is required" >&2; exit 1)
	@psql "$$DB_DSN" -v ON_ERROR_STOP=1 -f backend/scripts/rollback_rag_v2.sql
