#!/usr/bin/env bash
# 只读 /proc；CPU 单位为占用核数，host 为整机忙碌比例。无外部监控依赖。
set -euo pipefail
trap 'exit 0' TERM INT
server_pid="$1" k6_pid="$2"
ticks="$(getconf CLK_TCK)"
declare -A last_ticks=()
last_time='' last_total=0 last_idle=0
printf 'time,source,cpu,rss_bytes,available_bytes,total_bytes\n'
while kill -0 "$k6_pid" 2>/dev/null; do
  now="$(date +%s.%N)"
  read -r available total < <(awk '/MemAvailable:/{a=$2*1024} /MemTotal:/{t=$2*1024} END{printf "%.0f %.0f\n",a,t}' /proc/meminfo)
  read -r host_total host_idle < <(awk '/^cpu /{for(i=2;i<=9;i++)t+=$i; printf "%.0f %.0f\n",t,$5+$6}' /proc/stat)
  if [[ -n "$last_time" ]] && (( host_total > last_total )); then
    cpu="$(awk -v dt="$((host_total-last_total))" -v di="$((host_idle-last_idle))" 'BEGIN{print (dt-di)/dt}')"
    printf '%s,host,%s,0,%s,%s\n' "$now" "$cpu" "$available" "$total"
  fi
  for source in server k6; do
    case "$source" in server) pid="$server_pid" ;; k6) pid="$k6_pid" ;; esac
    [[ -r "/proc/$pid/stat" && -r "/proc/$pid/status" ]] || continue
    # 去掉包含空格的进程名；剩余第 12/13 项为 utime/stime。
    current="$(awk '{sub(/^.*\) /, "");print $12+$13}' "/proc/$pid/stat")" || continue
    rss="$(awk '/VmRSS:/{printf "%.0f",$2*1024}' "/proc/$pid/status")" || continue
    [[ -n "$rss" ]] || continue
    if [[ -n "${last_ticks[$source]:-}" && -n "$last_time" ]]; then
      cpu="$(awk -v used="$((current-last_ticks[$source]))" -v hz="$ticks" -v now="$now" -v prev="$last_time" 'BEGIN{print used/hz/(now-prev)}')"
      printf '%s,%s,%s,%s,%s,%s\n' "$now" "$source" "$cpu" "$rss" "$available" "$total"
    fi
    last_ticks[$source]="$current"
  done
  # Docker Desktop 的 PID 命名空间可能与 WSL 不同，必须在容器内读 mysqld。
  mysql_stat="$(docker compose exec -T mysql sh -c 'test "$(cat /proc/1/comm)" = mysqld && cat /proc/1/stat /proc/1/status')" || exit 1
  read -r current rss < <(awk 'NR==1{sub(/^.*\) /, "");t=$12+$13} /^VmRSS:/{r=$2*1024} END{printf "%.0f %.0f\n",t,r}' <<< "$mysql_stat")
  if [[ -n "${last_ticks[mysql]:-}" && -n "$last_time" ]]; then
    cpu="$(awk -v used="$((current-last_ticks[mysql]))" -v hz="$ticks" -v now="$now" -v prev="$last_time" 'BEGIN{print used/hz/(now-prev)}')"
    printf '%s,mysql,%s,%s,%s,%s\n' "$now" "$cpu" "$rss" "$available" "$total"
  fi
  last_ticks[mysql]="$current"
  last_time="$now" last_total="$host_total" last_idle="$host_idle"
  sleep 2
done
