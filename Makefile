APP := bluebell
BIN_DIR := bin

# make 命令行参数传给现有压测脚本；默认值仍由脚本维护。
export ENDPOINT RATE RAMP_SECONDS HOLD_SECONDS VUS LABEL RATES BEFORE AFTER

.DEFAULT_GOAL := help

.PHONY: help dev run build migrate test fmt vet tidy check up down clean \
	loadtest-init loadtest-smoke loadtest loadtest-baseline loadtest-reset \
	loadtest-probe loadtest-repeat loadtest-compare loadtest-report

help:
	@echo "Usage:"
	@echo "  make dev       启动开发服务（实时重载）"
	@echo "  make run       启动服务"
	@echo "  make build     编译项目"
	@echo "  make migrate   执行数据库迁移"
	@echo "  make test      运行测试（集成测试需先 make up）"
	@echo "  make check     格式化并检查代码"
	@echo "  make tidy      整理 Go 依赖"
	@echo "  make up        启动开发环境"
	@echo "  make down      停止开发环境"
	@echo "  make clean     清理编译产物"
	@echo ""
	@echo "压测（先 make up，首次使用再 make loadtest-init）："
	@echo "  make loadtest-init      初始化压测数据和快照，仅首次执行"
	@echo "  make loadtest-smoke     六个核心场景冒烟，失败即停止"
	@echo "  make loadtest           单接口压测，默认时间列表 10 RPS / 60 秒"
	@echo "  make loadtest-baseline  六个核心接口，各测 3 分钟参考负载"
	@echo "  make loadtest-probe     逐档加压，记录通过和失败档位"
	@echo "  make loadtest-repeat    指定 ENDPOINT、RATE、LABEL=before/after，各测 3 轮"
	@echo "  make loadtest-compare   指定 BEFORE、AFTER，各为 3 个目录，逗号分隔"
	@echo "  make loadtest-report    重新生成 HTML 总览"
	@echo "  make loadtest-reset     恢复压测 MySQL 和 Redis 数据"
	@echo "  例：make loadtest ENDPOINT=vote RATE=100 RAMP_SECONDS=0 HOLD_SECONDS=180"
	@echo "  报告：tests/load/output/results/index.html；说明：tests/load/README.md"
	@echo "  每次只运行一个压测命令；写接口还需检查 Outbox 是否持续积压。"

dev:
	air

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

loadtest-init:
	@tests/load/run.sh init

loadtest-smoke:
	@tests/load/run.sh smoke

loadtest:
	@tests/load/run.sh run

loadtest-baseline:
	@tests/load/run.sh baseline

loadtest-probe:
	@tests/load/run.sh probe

loadtest-repeat:
	@tests/load/run.sh repeat

loadtest-compare:
	@tests/load/run.sh compare

loadtest-report:
	@tests/load/run.sh report

loadtest-reset:
	@tests/load/run.sh reset
