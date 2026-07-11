# learning_buddy 常用命令

.PHONY: infra dev lint format migrate

## 仅起基础设施（db / redis / minio）
infra:
	docker compose up -d db redis minio

## 起全栈
dev:
	docker compose up

## 格式化（自动修复可安全格式化的内容）
format:
	cd backend && gofmt -w .
	cd frontend && npm run format
	cd agent && ruff format .

## 静态检查
lint:
	cd backend && golangci-lint run
	cd frontend && npm run lint
	cd agent && ruff check .

## 跑数据库迁移（占位：接入 golang-migrate / atlas 后实现）
migrate:
	@echo "TODO: 接入迁移工具（golang-migrate / atlas）后在此执行"
