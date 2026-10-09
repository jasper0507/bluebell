APP := bluebell
BIN_DIR := bin

# make 命令行参数传给现有压测脚本；默认值仍由脚本维护。
export ENDPOINT RATE RAMP_SECONDS HOLD_SECONDS VUS

.DEFAULT_GOAL := help

.PHONY: help dev run build migrate test fmt vet tidy check up down clean \
	loadtest-init loadtest-smoke loadtest loadtest-baseline loadtest-reset

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
	@echo "  make loadtest-smoke     19 个场景冒烟，失败即停止"
	@echo "  make loadtest           单接口压测，默认时间列表 10 RPS / 60 秒"
	@echo "  make loadtest-baseline  顺序复测六个核心基线，各 3 分钟，约 20 分钟"
	@echo "  make loadtest-reset     恢复压测 MySQL 和 Redis 数据"
	@echo "  例：make loadtest ENDPOINT=vote RATE=100 RAMP_SECONDS=0 HOLD_SECONDS=180"
	@echo "  结果：tests/load/output/results/；接口与参数：tests/load/README.md"
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

# 与 tests/load/baseline.md 中六个 180 秒稳定档位保持一致。
# 每条命令完成后才开始下一条；任何 HTTP 阈值失败都会停止。
loadtest-baseline:
	@echo "[1/6] 注册：125 RPS，持续 3 分钟"
	@ENDPOINT=signup RATE=125 VUS=99 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run
	@echo "[2/6] 登录：125 RPS，持续 3 分钟"
	@ENDPOINT=login RATE=125 VUS=99 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run
	@echo "[3/6] 帖子详情：1500 RPS，持续 3 分钟"
	@ENDPOINT=post RATE=1500 VUS=600 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run
	@echo "[4/6] 时间排序列表：1500 RPS，持续 3 分钟"
	@ENDPOINT=posts_time RATE=1500 VUS=600 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run
	@echo "[5/6] 发帖：100 RPS，持续 3 分钟"
	@ENDPOINT=create_post RATE=100 VUS=80 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run
	@echo "[6/6] 投票：100 RPS，持续 3 分钟"
	@ENDPOINT=vote RATE=100 VUS=80 RAMP_SECONDS=0 HOLD_SECONDS=180 tests/load/run.sh run

loadtest-reset:
	@tests/load/run.sh reset
