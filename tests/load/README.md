# 压测使用说明

日常只需运行 Make 命令，查看报告。命令自动编译、启动和停止压测服务，并使用独立 MySQL 库和 Redis DB13。

## 如何运行

在项目根目录执行。当前本机已经初始化，日常复测用：

```bash
make up                # 启动 MySQL、Redis，等待服务就绪
make loadtest-baseline # 六个核心场景，各3分钟，约20分钟
make loadtest-reset    # 恢复测试数据，报告会保留
```

只测某个接口时，用下面的命令替换中间一步：

```bash
make loadtest ENDPOINT=vote RATE=100
```

这表示每秒计划发出100次投票请求，稳定运行60秒。`ENDPOINT=post` 是帖子详情，`ENDPOINT=posts_time` 是时间排序列表，`ENDPOINT=login` 是登录。其他名称见 [基线报告](./baseline.md) 的“全业务接口基线”第一列。需要运行3分钟时，加 `HOLD_SECONDS=180`。

六场景基线总计施压18分钟，加上每轮数据准备及10秒恢复观察，约20分钟。日常检查单个接口可以缩短，例如：

```bash
make loadtest ENDPOINT=post RATE=100 VUS=50 RAMP_SECONDS=0 HOLD_SECONDS=60
```

| 参数 | 含义 |
| --- | --- |
| `ENDPOINT` | 选择接口 |
| `RATE` | 每秒计划发出的请求数，默认10 |
| `VUS` | 可用虚拟用户数，手动指定时支持任意正整数；省略时自动估算 |
| `HOLD_SECONDS` | 稳定施压秒数，默认60 |
| `RAMP_SECONDS` | 升到目标速率的秒数，默认10；0表示直接开始 |

当前按 `RATE` 控制发请求的速率；`VUS=50` 提供50个执行请求的槽位，空闲的VU会等待，不保证同时有50个请求。VU不够时可能出现 `dropped_iterations`，先增加 `VUS` 核对原因。[k6 官方 VU 分配说明](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/arrival-rate-vu-allocation/)

`make loadtest-reset` 把压测 MySQL 库 `bluebell_k6_verify` 恢复为快照中的200用户、5社区、2,000帖子、20,000评论、20,000投票，再清空并重建 Redis DB13 的投影和测试凭证。测试期间的新增、删除和投票改动会被恢复；业务库 `bluebell`、Redis DB0 和已有报告不参与恢复。

新环境首次使用时，在 `make up` 后执行：

```bash
make loadtest-init  # 仅首次执行，已有基线时跳过
make loadtest-smoke # 19个场景低负载验证，确认接口和数据正常
```

每次只运行一个压测命令。六场景基线的接口、速率和时长固定；自定义负载用 `make loadtest`。更多命令用 `make help` 查看。

## 如何看结果

**先打开本机报告索引 `tests/load/output/results/README.md`。** “六个核心稳定基线”用于比较优化前后；“瓶颈和连接池对照”用于理解哪些负载开始失败；“后续复测”会自动追加新报告链接。

每轮结束时，终端会打印结果目录和索引位置：

```text
tests/load/output/results/<时间>-<接口>-<速率>/
```

**先看终端的 `THRESHOLDS` 区域。** `phase:load` 表示稳定施压阶段，比较性能时看这一阶段。

| 指标 | 怎么理解 | 当前通过标准 |
| --- | --- | --- |
| `http_req_duration{phase:load}` 的 p95 | 约95%的请求耗时在该值以内 | < 500ms |
| `http_req_failed{phase:load}` | HTTP失败比例 | < 1% |
| `checks{phase:load}` | HTTP状态与业务响应正确的比例 | > 99% |
| `dropped_iterations` | 计划发出但没能启动的请求数 | 0 |

全部阈值显示 `✓` 表示 HTTP 测试通过；出现 `✗` 或 Make 报错，该轮需要排查。阈值失败也会保存报告。

**然后打开结果目录中的 `report.html`。** 文件位于每轮子目录中，例如本机已有的 `tests/load/output/results/20261009-180331-post-1500/report.html`。用浏览器查看请求速率和响应时间曲线。用相同命令比较优化前后的结果；历史数据见 [baseline.md](./baseline.md)。

默认图表采样周期10秒，建议稳定运行至少35秒再看HTML。短测试可能不生成 `report.html`，这时看终端或 `k6.log`；本机之前的5秒冒烟没有生成HTML。[k6 官方报告说明](https://grafana.com/docs/k6/latest/results-output/web-dashboard/)

**发帖、删帖和投票还要看 `outbox.csv`。** 用表格软件打开，按时间查看 `pending`（待处理数量）和 `oldest_seconds`（最老事件等待秒数）：持续施压时若不断增加、不能回落，说明后台处理跟不上，这档负载不算通过。停压后归零不能替代施压期间的检查。

终端摘要也保存在同目录的 `k6.log`，关闭终端后仍可查看。

每轮只保留以下核心输出，无需逐个翻其他文件：

| 文件 | 用途 |
| --- | --- |
| `report.html` | 浏览器查看性能曲线；短测试可能没有 |
| `k6.log` | 查看阈值、p95、错误/丢弃数和 HTTP 退出码 |
| `parameters.txt` | 比较两轮时确认参数一致 |
| `outbox.csv` | 仅发帖、删帖、投票保留，用于检查积压 |
| `resources.csv` | 排查时查看服务、k6、MySQL、Redis 的 CPU 和内存 |
| `summary.json` | 精确测量数值，日常不用打开 |

`resources.csv` 已合并容器采样；CPU 以一个核为100%，`host_cpu_percent` 为整机利用率，容器内存列记录使用量。历史瓶颈轮次的关键 SQL 快照放在 `diagnostics/`；后续测试不再默认采集 SQL 快照，异常时会在该目录保留警告、错误或崩溃信息。普通请求日志不长期保存；大量诊断日志只保留首尾各100行样本，并标记省略行数。

## 哪些文件不用管

下面这些文件保留即可，日常无需打开或手工修改：

| 文件 | 用途 |
| --- | --- |
| `config.yaml`、`api.js`、`run.sh`、`resources.py`、`data/` | 配置和实现，由 Make 命令调用 |
| `output/bin/`、`output/state/data.json` | 自动生成的程序和临时凭证 |
| `output/state/baseline.sql` | 固定测试数据快照，**需要保留** |
| 报告中的 `summary.json`、`resources.csv`、`diagnostics/` | 精确数值和故障定位资料，排查时再用 |
| `tmp/cleanup-backup/` | 业务数据清理备份，需要保留 |

这些 `output/` 路径均位于 `tests/load/` 下；`tmp/cleanup-backup/` 位于项目根目录。

测试流程参考 [k6 官方测试类型指导](https://grafana.com/docs/k6/latest/testing-guides/test-types/)。上述通过标准是本项目的初始指标；脚本使用 [k6 thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/) 判断 HTTP 测试结果。
