#!/usr/bin/env bash
# 只编排服务、固定数据恢复与 SQL 采样；压测和 HTML 报告由原生 k6 生成。
set -euo pipefail
cd "$(dirname "$0")/../.."
export BLUEBELL_CONFIG_FILE="$PWD/tests/load/config.yaml"
work="$PWD/tests/load/output"
state="$work/state"
bin="$work/bin"
results="$work/results"
mkdir -p "$state" "$bin" "$results"
chmod 700 "$work"
export DATA_FILE="$state/data.json"
exec 9>"$state/run.lock"
flock -n 9 || { echo '已有压测命令运行，请等它结束。' >&2; exit 1; }
server_pid='' observer_pid=''
cleanup() {
  for pid in "$observer_pid" "$server_pid"; do
    if [[ -n "$pid" ]]; then kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
  done
  server_pid='' observer_pid=''
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mysql() {
  docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot --batch --raw --skip-column-names --default-character-set=utf8mb4 bluebell_k6_verify'
}
port_free() {
  local listeners
  listeners="$(ss -ltnH '( sport = :18080 )')" || return 1
  [[ -z "$listeners" ]] || { echo '18080 已占用，请先停止压测服务。' >&2; return 1; }
}
build() {
  go build -o "$bin/data" ./tests/load/data || return
  go build -o "$bin/server" ./cmd/server
}
restore() {
  [[ -s "$state/baseline.sql" ]] || { echo '先执行 make loadtest-init。' >&2; exit 1; }
  port_free || return
  mysql < "$state/baseline.sql" || return
  "$bin/data" -mode prepare
}
observe() {
  trap 'exit 0' TERM INT
  printf 'time,pending,oldest,retries\n'
  while true; do
    mysql <<'SQL' | tr '\t' ',' || return
SELECT UNIX_TIMESTAMP(NOW(3)),COUNT(*),COALESCE(MAX(TIMESTAMPDIFF(MICROSECOND,created_at,NOW(3)))/1000000,0),COALESCE(SUM(retry_count),0) FROM outbox_events;
SQL
    sleep 2
  done
}
report() {
  REPORT_FILES="$1" REPORT_OUTPUT="$2" REPORT_MODE="${3:-index}" K6_WEB_DASHBOARD=false \
    k6 run --quiet --no-usage-report tests/load/report.js
}
run_one() {
  export ENDPOINT="${ENDPOINT:-posts_time}" RATE="${RATE:-10}" RAMP_SECONDS="${RAMP_SECONDS:-10}" HOLD_SECONDS="${HOLD_SECONDS:-60}" LABEL="${LABEL:-current}"
  case "$ENDPOINT" in signup|login|post|posts_time|create_post|vote) ;; *) echo "未知 ENDPOINT: $ENDPOINT" >&2; return 1 ;; esac
  for value in "$RATE" "$RAMP_SECONDS" "$HOLD_SECONDS" "${VUS:-10}"; do
    [[ "$value" =~ ^(0|[1-9][0-9]*)$ ]] || { echo '负载参数必须为整数，不能有前导零。' >&2; return 1; }
  done
  (( RATE > 0 && HOLD_SECONDS > 0 )) || { echo 'RATE 和 HOLD_SECONDS 必须大于 0。' >&2; return 1; }
  [[ "$LABEL" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo 'LABEL 只允许字母、数字、下划线和连字符。' >&2; return 1; }
  local slots="${VUS:-$(( (RATE * 3 + 3) / 4 + 5 ))}"
  [[ -n "${VUS:-}" ]] || { (( slots >= 10 )) || slots=10; }
  (( slots > 0 )) || { echo 'VUS 必须大于 0。' >&2; return 1; }
  restore || return
  export RUN_ID="$(date +%Y%m%d%H%M%S%N)" RESULT_DIR="$results/$(date +%Y%m%d-%H%M%S%N)-$LABEL-$ENDPOINT-$RATE"
  mkdir -p "$RESULT_DIR" || return
  export LOAD_KERNEL="$(uname -sr)" LOAD_CPU="$(awk -F': ' '/model name/{print $2;exit}' /proc/cpuinfo)"
  export LOAD_CORES="$(nproc)" LOAD_MEMORY_KIB="$(awk '/MemTotal/{print $2}' /proc/meminfo)"
  export LOAD_GO="$(go version)" LOAD_K6="$(k6 version)" LOAD_COMMIT="$(git rev-parse HEAD)"
  export LOAD_CODE_HASH="$(sha256sum "$bin/server" | cut -d' ' -f1)"
  # 离线 HTML 排版不影响施压条件，不纳入测量脚本指纹。
  export LOAD_SCRIPT_HASH="$(sha256sum tests/load/api.js tests/load/run.sh tests/load/data/main.go | sha256sum | cut -d' ' -f1)"
  export LOAD_SNAPSHOT_HASH="$(sha256sum "$state/baseline.sql" | cut -d' ' -f1)"
  LOAD_MYSQL="$(mysql <<< 'SELECT VERSION();')" || return
  LOAD_REDIS="$(docker compose exec -T redis redis-server --version)" || return
  export LOAD_MYSQL LOAD_REDIS
  "$bin/server" > "$RESULT_DIR/server.log" 2>&1 & server_pid=$!
  local ready=false
  for (( i=0; i<50; i++ )); do
    if curl -fsS --max-time 1 http://127.0.0.1:18080/health >/dev/null 2>&1; then ready=true; break; fi
    if ! kill -0 "$server_pid" 2>/dev/null; then tail -n 30 "$RESULT_DIR/server.log" >&2; return 1; fi
    sleep 0.1
  done
  "$ready" || { echo '压测服务启动超时。' >&2; return 1; }
  if [[ "$ENDPOINT" == create_post || "$ENDPOINT" == vote ]]; then
    observe > "$RESULT_DIR/outbox.csv" 2> "$RESULT_DIR/observer.log" & observer_pid=$!
  fi
  local status=0
  VUS="$slots" K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_PORT=-1 K6_WEB_DASHBOARD_PERIOD=2s \
    K6_WEB_DASHBOARD_EXPORT="$RESULT_DIR/charts.html" \
    k6 run --quiet --no-usage-report tests/load/api.js > "$RESULT_DIR/k6.log" 2>&1 || status=$?
  printf '%s\n' "$status" > "$RESULT_DIR/exit-code.txt"
  if [[ -n "$observer_pid" ]]; then
    sleep 10
    if ! kill -0 "$observer_pid" 2>/dev/null; then
      echo 'Outbox SQL 采样失败，本轮无效。' >&2
      cat "$RESULT_DIR/observer.log" >&2
      status=1
      printf '1\n' > "$RESULT_DIR/exit-code.txt"
    fi
  fi
  cleanup
  # 只保留诊断日志，不长期保存所有 info 请求日志。
  awk '/"level":"(WARN|ERROR|FATAL)"/ || !/^\{/' "$RESULT_DIR/server.log" | tail -n 200 > "$RESULT_DIR/server.filtered"
  mv "$RESULT_DIR/server.filtered" "$RESULT_DIR/server.log"
  tail -n 8 "$RESULT_DIR/k6.log"
  [[ -s "$RESULT_DIR/summary.json" ]] || { echo "未生成摘要，请检查 $RESULT_DIR/k6.log" >&2; return 1; }
  printf '%s\n' "$RESULT_DIR/summary.json" >> "$results/current-runs.txt"
  report "$(paste -sd, "$results/current-runs.txt")" "$results/index.html" || return
  LAST_RESULT="$RESULT_DIR/summary.json"
  echo "本轮报告：$RESULT_DIR/report.html"
  if (( status == 0 && HOLD_SECONDS >= 30 )) && [[ "$(cat "$RESULT_DIR/verdict.txt")" != 0 ]]; then status=99; fi
  return "$status"
}

case "${1:-help}" in
  init)
    [[ ! -f "$state/baseline.sql" ]] || { echo '已有快照；使用 make loadtest-reset，避免覆盖。' >&2; exit 1; }
    port_free
    docker compose up -d mysql redis
    ready=false
    for (( i=0; i<40; i++ )); do
      if docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -e "SELECT 1"' >/dev/null 2>&1; then ready=true; break; fi
      sleep 1
    done
    "$ready" || { echo 'MySQL 启动超时。' >&2; exit 1; }
    docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot' <<'SQL'
CREATE DATABASE IF NOT EXISTS bluebell_k6_verify CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'loadtest'@'%' IDENTIFIED BY 'loadtest_local_only';
GRANT ALL PRIVILEGES ON bluebell_k6_verify.* TO 'loadtest'@'%';
SQL
    build
    "$bin/data" -mode seed
    docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --skip-comments bluebell_k6_verify' > "$state/baseline.sql"
    chmod 600 "$state/baseline.sql"
    "$bin/data" -mode prepare
    ;;
  reset) build; restore ;;
  run) build; run_one ;;
  smoke|baseline)
    build
    batch=''
    for endpoint in signup login post posts_time create_post vote; do
      if [[ "$1" == smoke ]]; then rate=1 hold=10; else
        hold=180
        case "$endpoint" in signup|login|create_post|vote) rate=100 ;; *) rate=1000 ;; esac
      fi
      ENDPOINT="$endpoint" RATE="$rate" HOLD_SECONDS="$hold" RAMP_SECONDS=0 run_one
      batch="${batch:+$batch,}$LAST_RESULT"
    done
    report "$batch" "$results/$1-$(date +%Y%m%d-%H%M%S).html"
    ;;
  probe)
    build
    case "${ENDPOINT:-all}" in all) endpoints='signup login post posts_time create_post vote' ;; *) endpoints="$ENDPOINT" ;; esac
    batch=''
    for endpoint in $endpoints; do
      if [[ -n "${RATES:-}" ]]; then rates="$RATES"; else
        case "$endpoint" in signup|login) rates='100 150 200' ;; post|posts_time) rates='1000 1500 2000 2500' ;; create_post|vote) rates='100 200 300 500' ;; *) echo '未知接口。' >&2; exit 1 ;; esac
      fi
      for rate in $rates; do
        status=0
        # 用 || 接住预期阈值失败；run_one 内部的关键操作必须显式检查退出码。
        ENDPOINT="$endpoint" RATE="$rate" HOLD_SECONDS=60 RAMP_SECONDS=10 run_one || status=$?
        (( status == 0 || status == 99 )) || exit "$status"
        batch="${batch:+$batch,}$LAST_RESULT"
        if (( status == 99 )); then echo "$endpoint 在 $rate RPS 未通过，停止该接口加压。"; break; fi
      done
    done
    report "$batch" "$results/probe-$(date +%Y%m%d-%H%M%S).html"
    ;;
  repeat)
    [[ -n "${ENDPOINT:-}" && -n "${RATE:-}" ]] || { echo '指定 ENDPOINT、RATE 和 LABEL=before/after。' >&2; exit 1; }
    [[ "${LABEL:-}" == before || "${LABEL:-}" == after ]] || { echo 'LABEL 必须是 before 或 after。' >&2; exit 1; }
    build
    batch='' status=0
    for (( round=1; round<=3; round++ )); do
      echo "正式测量 $LABEL 第 $round/3 轮"
      current=0
      HOLD_SECONDS=180 run_one || current=$?
      (( current == 0 || current == 99 )) || exit "$current"
      (( current == 0 )) || status=99
      batch="${batch:+$batch,}$LAST_RESULT"
    done
    report "$batch" "$results/$LABEL-$(date +%Y%m%d-%H%M%S).html"
    exit "$status"
    ;;
  compare)
    [[ -n "${BEFORE:-}" && -n "${AFTER:-}" ]] || { echo '指定 BEFORE 和 AFTER，各为三个结果目录（逗号分隔）。' >&2; exit 1; }
    files=''
    IFS=',' read -ra dirs <<< "$BEFORE,$AFTER"
    for dir in "${dirs[@]}"; do
      path="$(realpath "$dir/summary.json")"
      files="${files:+$files,}$path"
    done
    report "$files" "$results/comparison-$(date +%Y%m%d-%H%M%S).html" compare
    ;;
  report)
    [[ -s "$results/current-runs.txt" ]] || { echo '暂无新版结果。' >&2; exit 1; }
    report "$(paste -sd, "$results/current-runs.txt")" "$results/index.html"
    ;;
  *) echo '使用 make loadtest / loadtest-smoke / loadtest-probe / loadtest-repeat / loadtest-compare / loadtest-reset。' ;;
esac
