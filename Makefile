# AI Employee Platform — 根构建入口
# 设计依据：docs/DEVELOPMENT_TASKS.md M0/M1+

.PHONY: help proto proto-lint test test-server test-workstation build build-server build-workstation build-admin tidy migrate-up migrate-status ci

help:
	@echo "常用目标:"
	@echo "  make tidy / test / build / proto / proto-lint / migrate-up / ci"

tidy:
	cd gen && go mod tidy
	cd server && go mod tidy
	cd workstation && go mod tidy

test: test-server test-workstation test-gen

test-gen:
	cd gen && go test ./...

test-server:
	cd server && go test ./...

test-workstation:
	cd workstation && go test ./...

build: build-server build-workstation

build-server:
	cd server && go build -o bin/server$(EXE) ./cmd/server
	cd server && go build -o bin/migrate$(EXE) ./cmd/migrate

build-workstation:
	cd workstation && go build -o bin/aew$(EXE) ./cmd/aew
	mkdir -p bin && cp -f workstation/bin/aew$(EXE) bin/aew$(EXE)

build-admin:
	cd admin && npm run build

# 在 proto/ 目录执行 buf generate，产物写入 gen/go
proto:
	cd proto && buf generate

proto-lint:
	cd proto && buf lint

# 需要本机或 docker 中的 Postgres，DSN 可用 AIE_DATABASE_URL 覆盖
migrate-up:
	cd server && go run ./cmd/migrate -dir ../migrations up

migrate-status:
	cd server && go run ./cmd/migrate -dir ../migrations status

ci: proto-lint proto tidy test build-server build-workstation
	@echo "CI Go+proto 阶段通过。Admin: cd admin && npm ci && npm run build"

ifeq ($(OS),Windows_NT)
EXE := .exe
else
EXE :=
endif
