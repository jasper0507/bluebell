#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

export BLUEBELL_CONFIG_FILE="$PWD/tests/load/config.yaml"
work="$PWD/tests/load/output"
state="$work/state"
bin="$work/bin"
mkdir -p "$state" "$bin" "$work/results"
chmod 700 "$work"
export DATA_FILE="$state/data.json"

mysql() {
  docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot --default-character-set=utf8mb4 bluebell_k6_verify'
}
port_free() {
  if [[ -n "$(ss -ltnH '( sport = :18080 )')" ]]; then
    echo '18080 已被占用；先停止该压测服务，避免重置时仍有写入。' >&2
    exit 1
  fi
}
restore() {
  [[ -f "$state/baseline.sql" ]] || { echo '先执行 make loadtest-init' >&2; exit 1; }
  port_free
  mysql < "$state/baseline.sql"
}

case "${1:-help}" in
  init)
    [[ ! -f "$state/baseline.sql" ]] || { echo '基线已存在；用 make loadtest-reset 恢复，避免覆盖。' >&2; exit 1; }
    port_free
    docker compose up -d mysql redis
    ready=false
    for (( i=0; i<40; i++ )); do
      if docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -e "SELECT 1"' >/dev/null 2>&1; then ready=true; break; fi
      sleep 1
    done
    "$ready" || { echo 'MySQL 启动超时' >&2; exit 1; }
    # 凭据只用于本地；此账号没有业务库 bluebell 的权限。
    docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot' <<'SQL'
CREATE DATABASE IF NOT EXISTS bluebell_k6_verify CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'loadtest'@'%' IDENTIFIED BY 'loadtest_local_only';
GRANT ALL PRIVILEGES ON bluebell_k6_verify.* TO 'loadtest'@'%';
SQL
    go build -o "$bin/data" ./tests/load/data
    go build -o "$bin/server" ./cmd/server
    "$bin/data" -mode seed
    docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --skip-comments bluebell_k6_verify' > "$state/baseline.sql"
    chmod 600 "$state/baseline.sql"
    "$bin/data" -mode prepare
    echo "基线已保存：$state/baseline.sql"
    ;;
  reset)
    port_free
    # 编译产物可以清理；恢复数据不依赖上次构建的二进制。
    go build -o "$bin/data" ./tests/load/data
    restore
    "$bin/data" -mode prepare
    ;;
  smoke)
    for endpoint in signup login refresh logout communities community posts_time posts_hot posts_community_time posts_community_hot post create_post delete_post get_vote vote comments create_comment reply delete_comment; do
      ENDPOINT="$endpoint" RATE=1 RAMP_SECONDS=0 HOLD_SECONDS=5 VUS=10 "$0" run
    done
    ;;
  run)
    export ENDPOINT="${ENDPOINT:-posts_time}"
    export RATE="${RATE:-10}"
    export RAMP_SECONDS="${RAMP_SECONDS:-10}"
    export HOLD_SECONDS="${HOLD_SECONDS:-60}"
    for value in "$RATE" "$RAMP_SECONDS" "$HOLD_SECONDS" "${VUS:-10}"; do
      [[ "$value" =~ ^[0-9]+$ ]] || { echo '负载参数必须是非负整数' >&2; exit 1; }
    done
    (( RATE > 0 && HOLD_SECONDS > 0 )) || { echo 'RATE 和 HOLD_SECONDS 必须大于0' >&2; exit 1; }
    case "$ENDPOINT" in
      signup|login|refresh|logout|communities|community|posts_time|posts_hot|posts_community_time|posts_community_hot|post|create_post|delete_post|get_vote|vote|comments|create_comment|reply|delete_comment) ;;
      *) echo "未知 ENDPOINT: $ENDPOINT" >&2; exit 1 ;;
    esac
    if [[ -z "${VUS:-}" ]]; then
      export VUS=$(( (RATE * 3 + 3) / 4 + 5 ))
      (( VUS >= 10 )) || export VUS=10
    else
      export VUS
    fi
    (( VUS > 0 )) || { echo 'VUS 必须是正整数' >&2; exit 1; }
    # pool 包含余量；初始化、鉴权、删除资源都在计时前准备。
    pool=$(( RATE * (RAMP_SECONDS + HOLD_SECONDS + 5) ))
    port_free
    # 每轮编译当前代码，避免优化代码后仍测旧二进制；Go 自带增量缓存。
    go build -o "$bin/data" ./tests/load/data
    go build -o "$bin/server" ./cmd/server
    restore
    "$bin/data" -mode prepare -endpoint "$ENDPOINT" -pool "$pool" -vus "$VUS"
    result="$work/results/$(date +%Y%m%d-%H%M%S)-$ENDPOINT-$RATE"
    mkdir -p "$result"
    printf 'ENDPOINT=%s\nRATE=%s\nRAMP_SECONDS=%s\nHOLD_SECONDS=%s\nVUS=%s\nPOOL=%s\n' "$ENDPOINT" "$RATE" "$RAMP_SECONDS" "$HOLD_SECONDS" "$VUS" "$pool" > "$result/parameters.txt"
    python3 - "$DATA_FILE" >> "$result/parameters.txt" <<'PY'
import json,sys
print('MYSQL_MAX_OPEN_CONNS=' + str(json.load(open(sys.argv[1]))['meta'][0]['max_open_conns']))
PY
    mkdir -p "$result/diagnostics"
    server_pid='' observer_pid='' stats_pid=''
    cleanup() {
      for pid in "$observer_pid" "$stats_pid" "$server_pid"; do
        if [[ -n "$pid" ]]; then kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
      done
      # 普通请求日志不长期保存；诊断日志保留首尾各100行样本。
      python3 - "$result/diagnostics" <<'PY'
import json,sys
from collections import deque
from pathlib import Path
directory=Path(sys.argv[1])
log=directory/'server.log'
if log.exists():
    temporary=directory/'server.filtered'
    first=[]
    last=deque(maxlen=100)
    count=0
    with log.open() as source:
        for line in source:
            try:
                keep=json.loads(line).get('level') in ('WARN', 'WARNING', 'ERROR', 'FATAL')
            except (ValueError, AttributeError):
                keep=True
            if keep:
                count+=1
                if len(first)<100:
                    first.append(line)
                else:
                    last.append(line)
    with temporary.open('w') as target:
        target.writelines(first)
        if count>200:
            target.write(f'\n[省略 {count-200} 行诊断日志；仅保留首尾各100行样本]\n\n')
        target.writelines(last)
    temporary.replace(log)
for path in directory.iterdir():
    if path.stat().st_size == 0:
        path.unlink()
if not any(directory.iterdir()):
    directory.rmdir()
PY
    }
    trap cleanup EXIT
    "$bin/server" > "$result/diagnostics/server.log" 2>&1 & server_pid=$!
    ready=false
    for (( i=0; i<50; i++ )); do
      if curl -fsS --max-time 1 http://127.0.0.1:18080/health > /dev/null 2>&1; then ready=true; break; fi
      kill -0 "$server_pid" 2>/dev/null || { cat "$result/diagnostics/server.log"; exit 1; }
      sleep 0.1
    done
    "$ready" || { cat "$result/diagnostics/server.log"; echo '压测服务未就绪' >&2; exit 1; }
    "$bin/data" -mode observe > "$result/outbox.csv" 2> "$result/diagnostics/observer.log" & observer_pid=$!
    # 系统和各进程 CPU/RSS；统计进程在 trap 中一起回收。
    python3 tests/load/resources.py "$server_pid" "$result/resources.csv" & stats_pid=$!
    export K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_PORT=-1
    export K6_WEB_DASHBOARD_EXPORT="$result/report.html"
    set +e
    k6 run --no-usage-report --summary-export "$result/summary.json" tests/load/api.js > "$result/k6.log" 2>&1
    status=$?
    set -e
    # 留 10 秒观察停止施压后的 Outbox 恢复，不算入 HTTP 指标。
    sleep 10
    kill -0 "$observer_pid" 2>/dev/null || { echo 'Outbox 观察进程失败' >&2; cat "$result/diagnostics/observer.log"; exit 1; }
    cleanup
    trap - EXIT
    printf '\nk6 exit code: %s\n' "$status" >> "$result/k6.log"
    cat "$result/k6.log"
    python3 - "$result/outbox.csv" <<'PY'
import csv,sys
rows=list(csv.DictReader(open(sys.argv[1])))
print('Outbox: peak=%s final=%s oldest_final=%ss' % (max(int(r['pending']) for r in rows), rows[-1]['pending'], rows[-1]['oldest_seconds']))
print('写接口还需检查 outbox.csv 的稳定阶段是否持续积压；k6 退出码只表示 HTTP 阈值结果。')
PY
    case "$ENDPOINT" in
      create_post|delete_post|vote) ;;
      *) rm "$result/outbox.csv" ;;
    esac
    index="$work/results/README.md"
    if [[ ! -f "$index" ]]; then
      printf '# 压测报告索引\n\nHTTP 通过后，写接口仍需检查 outbox.csv 是否持续积压。\n\n| 轮次 | HTTP 退出码（0为通过） | 报告 | 终端摘要 | 参数 |\n| --- | --- | --- | --- | --- |\n' > "$index"
    fi
    name="${result##*/}"
    report_link='无 HTML（短测试）'
    [[ ! -f "$result/report.html" ]] || report_link="[打开]($name/report.html)"
    printf '| %s | %s | %s | [查看](%s/k6.log) | [查看](%s/parameters.txt) |\n' "$name" "$status" "$report_link" "$name" "$name" >> "$index"
    echo "结果：$result"
    echo "报告索引：$index"
    exit "$status"
    ;;
  *)
    echo 'make loadtest-init                    初始化/复用种子并保存基线'
    echo 'make loadtest-smoke                   全接口低负载验证'
    echo 'make loadtest ENDPOINT=post RATE=50'
    echo 'make loadtest-reset                   恢复压测库，重建 Redis DB13'
    ;;
esac
