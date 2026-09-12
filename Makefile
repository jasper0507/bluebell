APP := bluebell
BIN_DIR := bin

.DEFAULT_GOAL := help

.PHONY: help run build migrate test fmt vet tidy check up down clean

help:
	@echo "Usage:"
	@echo "  make run       启动服务"
	@echo "  make build     编译项目"
	@echo "  make migrate   执行数据库迁移"
	@echo "  make test      运行测试"
	@echo "  make check     格式化并检查代码"
	@echo "  make tidy      整理 Go 依赖"
	@echo "  make up        启动开发环境"
	@echo "  make down      停止开发环境"
	@echo "  make clean     清理编译产物"

run:
	go run ./cmd/server

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP) ./cmd/server

migrate:
	go run ./cmd/migrate

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

check: fmt vet test

up:
	docker compose up -d

down:
	docker compose down

clean:
	rm -rf $(BIN_DIR)
