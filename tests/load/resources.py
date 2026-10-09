"""每两秒采样 Linux CPU 和进程内存，不安装额外监控服务。"""
import csv
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import threading
import time

server_pid = int(sys.argv[1])
running = True

def stop(*_):
    global running
    running = False

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
ticks = os.sysconf('SC_CLK_TCK')
previous = {}
previous_cpu = None
previous_time = time.monotonic()
# 容器和本机进程采样写入同一份 CSV。
stats = subprocess.Popen(['docker', 'stats', '--format', '{{json .}}',
                          'bluebell-mysql-1', 'bluebell-redis-1'],
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)

lock = threading.Lock()

def containers():
    units = {'B': 1, 'kB': 1000, 'MB': 1000**2, 'GB': 1000**3,
             'KiB': 1024, 'MiB': 1024**2, 'GiB': 1024**3}
    for line in stats.stdout:
        line = re.sub(r'\x1b\[[0-9;]*[A-Za-z]', '', line)
        try:
            record = json.loads(line)
            value, unit = re.fullmatch(r'([\d.]+)\s*([A-Za-z]+)',
                                      record['MemUsage'].split('/')[0].strip()).groups()
            rss = round(float(value)*units[unit]/1024**2, 1)
            used = float(record['CPUPerc'].rstrip('%'))
        except (ValueError, KeyError, AttributeError):
            continue
        with lock:
            writer.writerow([time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                             '', record['Name'], '', used, rss])
            output.flush()

with open(sys.argv[2], 'w') as output:
    writer = csv.writer(output)
    writer.writerow(['time', 'host_cpu_percent', 'process', 'pid', 'cpu_percent', 'rss_mib'])
    thread = threading.Thread(target=containers, daemon=True)
    thread.start()
    while running:
        now = time.monotonic()
        cpu = list(map(int, Path('/proc/stat').read_text().splitlines()[0].split()[1:9]))
        busy = ''
        if previous_cpu:
            delta = [a-b for a, b in zip(cpu, previous_cpu)]
            busy = round(100*(sum(delta)-delta[3]-delta[4])/max(1, sum(delta)), 1)
        previous_cpu = cpu
        for path in Path('/proc').glob('[0-9]*'):
            try:
                pid = int(path.name)
                name = (path/'comm').read_text().strip()
                if pid != server_pid and name not in ('k6', 'mysqld', 'redis-server'):
                    continue
                fields = (path/'stat').read_text().rsplit(')', 1)[1].split()
                consumed = int(fields[11]) + int(fields[12])
                used = round(100*(consumed-previous[pid])/ticks/(now-previous_time), 1) if pid in previous else ''
                previous[pid] = consumed
                rss = round(int(fields[21])*os.sysconf('SC_PAGE_SIZE')/1024**2, 1)
                with lock:
                    writer.writerow([time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), busy, name, pid, used, rss])
            except (OSError, ValueError, IndexError):
                continue
        with lock:
            output.flush()
        previous_time = now
        time.sleep(2)
    stats.terminate()
    stats.wait()
    thread.join()
