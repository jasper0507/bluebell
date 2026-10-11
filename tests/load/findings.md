# 当前压测发现

初次探测日期：2026-10-10；Outbox 批量优化 A/B 日期：2026-10-11，结果见本文末尾。以下先保留六核心场景脚本的初次探测结果，不是历史 2026-10-09 基线。每档升速 10 秒、稳定施压 60 秒，每次迭代只有一个目标请求。通过条件：p95 < 500ms、HTTP 和业务失败率均 < 1%、零丢弃；写接口还需后台未见持续积压。

HTML 探测总览：[打开图表](./output/results/probe-20261010-163945.html)。全部新版轮次见 [HTML 索引](./output/results/index.html)。原始结果在本机被忽略的 output 目录，保留本页数值用于追溯；完整使用方式见 [README](./README.md)。

## 六接口的已测区间

| 接口 | 最高已测通过 RPS | 通过档位 p95 ms | 首次未通过 RPS | 未通过的证据 |
| --- | ---: | ---: | ---: | --- |
| 注册 | 100 | 63.46 | 150 | p95 1836.35ms，丢弃 1114 次 |
| 登录 | 100 | 48.17 | 150 | p95 2126.12ms，丢弃 839 次 |
| 帖子详情 | 2000 | 177.98 | 2500 | p95 1685.00ms，丢弃 21197 次 |
| 时间列表 | 2500 | 159.81 | 未找到 | 本轮最高测试 2500；不推断精确上限 |
| 发帖 | 100 | 24.22 | 200 | HTTP p95 28.00ms，但后台积压 |
| 投票 | 100 | 23.39 | 200 | HTTP p95 29.57ms，丢弃 5 次且后台积压 |

通过数字是短测下界，不是生产容量，接口 RPS 不能相加。有丢弃的轮次不是精确后端吞吐上限。

## 优先处理写链路

200 RPS 的 HTTP 性能看似正常，但稳定阶段 SQL 采样显示：

| 接口 | 实际成功 RPS | 待处理事件峰值 | 最老事件等待峰值 | 停压恢复后待处理 |
| --- | ---: | ---: | ---: | ---: |
| 发帖 | 200.00 | 4179 | 20.910 秒 | 0 |
| 投票 | 199.92 | 7410 | 37.089 秒 | 0 |

待处理数量与等待年龄在施压期间增长，证明后台投影链路在该档不能按标准通过；停压后归零不能替代这一检查。这里只能定位链路，不能断言是 Redis 本身慢。测量时 Worker 使用 4 路并发，不应沿用旧报告的串行处理归因。当前代码已改为批量查询和 Redis Pipeline 同步；以上探测数字仍属于优化前版本，批量优化的正式对照见末尾。

优先根据慢操作日志核对 Outbox 确认删除；连接池等待和投影往返仍是候选方向，需独立验证。改连接池属于配置对照，不能把收益全部归因于代码。

帖子详情也值得检查：`internal/service/post.go` 中详情依次读取帖子、作者、社区和票数；相比时间列表，它更早出现延迟上升。减少查询往返是候选方向，尚未做性能因果验证。

## 复现条件

- Linux 6.18.40.1-microsoft-standard-WSL2，13th Gen Intel(R) Core(TM) i9-13900HX，可用 6 个逻辑 CPU，内存约 3.83 GiB；后端、k6、MySQL 与 Redis 共享本机硬件。
- go version go1.27.0 linux/amd64；k6 v2.3.0 (commit/e088784614, go1.26.8, linux/amd64)；MySQL 8.4.9；Redis 7.4.9。
- MySQL 最大/空闲连接数均为 10，info 请求日志，Worker 并发 4。初始化与真实登录在计时外，发帖正文 4 KiB。
- 每轮恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票，并重建 Redis DB13 投影。数据库为 bluebell_k6_verify，服务端口 18080。
- 源提交 `4b0383b6b482cc7c0837cb7db6aad7b3cb41a49d`，含未提交的压测重构；服务程序指纹 `3a9ff470cf8a3ac8a71611a23c2db67ac6459ee6f8480fc30d7b61409b794575`。每轮具体脚本、配置和快照指纹见 HTML 的“测试条件”。

上述初次探测没有优化后样本。优化收益以本文末尾同条件重测的前后三轮对照为准。

## 定稿脚本的三轮优化前基线

[打开三轮 HTML](./output/results/before-20261010-170402.html)。以下三轮使用相同定稿测量指纹、代码、快照和配置：发帖 200 RPS、155 VU、升速 10 秒、稳定 180 秒、连接池 10、Worker 并发 4。此前报告定稿期间的诊断轮次保留供排查。这三个目录仅能与相同测量指纹的结果配对；本次数据准备工具和服务版本选择脚本已变化，因此另测新的优化前三轮。

| 轮次 | 成功 RPS | p95 ms | HTTP / 业务失败率 | 丢弃 | 后台待处理峰值 | 最老事件等待峰值 秒 | 后台判断 |
| --- | ---: | ---: | --- | ---: | ---: | ---: | --- |
| [1](./output/results/20261010-165316558587136-before-create_post-200/report.html) | 200.01 | 27.88 | 0.000% / 0.000% | 0 | 7206 | 36.049 | 持续积压 |
| [2](./output/results/20261010-165654372187755-before-create_post-200/report.html) | 199.99 | 27.86 | 0.006% / 0.006% | 0 | 4559 | 22.808 | 需确认 |
| [3](./output/results/20261010-170032203723592-before-create_post-200/report.html) | 200.00 | 30.63 | 0.000% / 0.000% | 0 | 6606 | 33.046 | 持续积压 |

三轮 HTTP p95 中位数 **27.88ms**；最老待处理事件等待峰值的中位数 **33.046 秒**，均未通过后台检查。两秒采样可能漏掉短暂峰值；该指标不是每条事件完成耗时的 p95。当时暂无优化后数据；本次收益不使用这组历史数据计算。第二轮期间做了少量只读 SQL 诊断，保留全部轮次，未挑选最优样本。

服务诊断日志提供了比 HTTP 更具体的线索：`internal/repository/outbox.go:68` 的批量确认删除，在之前三轮诊断样本中最长耗时分别为 23.702、35.281、25.913 秒（日志只保留最后 200 行，不能当作完整分布）。当前一次 EXPLAIN 对 256 个待处理 ID 显示 PRIMARY/range；只读采样没有捕获行锁等待，另看到一个运行约 14 秒的 updating 预处理语句，但未直接绑定到该删除。**尚未确定是执行计划变化、锁等待、存储开销或连接等待；不把具体原因写成已证实结论。**

下一步优先核对确认删除，再验证小批次确认能否降低后台等待；每次只改一个变量。保持连接池和 Worker 并发不变，优化后按相同参数各测三轮，再用 HTML 对照 p95、成功 RPS、后台积压数量和等待峰值。

## 逐轮记录与正确性验证（2026-10-10）

已将本机仍保留的全部 44 轮结果分类归档：[新版台账](./records.csv) 28 轮、[历史台账](./records-history.csv) 16 轮。新版包含 6 轮冒烟、16 轮容量探测、6 轮 `before`；其中前 3 轮是脚本定稿期间的诊断，正式优化前基线只使用上节列出的后 3 轮。暂无 `after`，本次不计算提升比例。历史上已删除的轮次只有基线文档中的汇总，不能伪造逐轮原始记录。

本次仅补充离线 CSV 导出和记录规则，没有修改服务、施压脚本、连接池或 Worker。离线 `report.js` 不参与测量脚本指纹，现有正式基线仍可用于后续相同条件对照。全量台账随 `make loadtest-report` 或每轮正常生成的完整总览刷新；批次报告和前后对照不覆盖全量台账。原始文件继续保留在被 Git 忽略的 output 目录，迁移机器时需另行复制。

| 验证分类 | 命令 / 方法 | 结果与限制 |
| --- | --- | --- |
| 报告工具 | `make loadtest-report` | 退出码 0，生成 HTML 总览及 28 行新版 CSV；这是离线重建，没有新增压测样本 |
| 数据核对 | Python 读取两份 CSV、全部 `summary.json`、退出码及 `outbox.csv` 逐项断言 | 退出码 0，44 轮完整覆盖且无重复；新版指标、环境、配置、指纹一致；后台峰值只取稳定阶段；历史 p95 一致 |
| 格式隔离 | 局部报告首次校验误选旧格式摘要 | 退出码 1，报告工具明确拒绝旧格式；这是校验输入选择错误，不是服务性能失败 |
| 覆盖保护 | 指定一个新版摘要，执行 `k6 run --quiet --no-usage-report tests/load/report.js`；`REPORT_OUTPUT=/tmp/bluebell-ledger-batch.html`，前后比较台账 SHA-256 | 退出码 0，局部 HTML 生成且全量 CSV 未被覆盖；其他参数为 `REPORT_MODE=index`、`K6_WEB_DASHBOARD=false` |
| 文档 / 补丁 | `git diff --check` | 退出码 0；本次只改离线报告与文档，未重跑业务集成测试 |

后续每项优化在此追加改动范围、准确命令、全部轮次目录及结果；最终同时对照最初基线与最终版本，分别给出绝对值、提升百分比和简历表述。三轮取中位数，不删除失败样本，不累加各阶段百分比。

## Outbox 批量同步测试适配（2026-10-11）

测试迁移到 `SyncPosts`，原 4 路并发断言改为批量查询次数和事件确认断言。补充帖子与用户的精确匹配、多用户投票变更、幂等重放、删除优先级、Redis 单帖失败与恢复、MySQL 整批失败与恢复、取消保留通知，以及 257 条通知分成 256/1 两批的覆盖。重试恢复用例用 MySQL 当前时间设置到期时间，避免 Go 与数据库时区不同导致误判。

压测准备工具同步迁移为每 256 个帖子调用一次 `SyncPosts`，每帖合并索引初始化和全部投票；失败即停止准备。本次未修改业务代码、连接池配置或施压参数，未实际运行压测准备工具及 k6，没有新增性能样本或收益百分比。

| 验证分类 | 命令 | 结果与限制 |
| --- | --- | --- |
| 全量回归 | `GOCACHE=/tmp/bluebell-go-cache go test ./... -count=1` | 退出码 0；真实 MySQL/Redis 集成测试通过，压测数据工具编译通过 |
| 竞态检查 | `GOCACHE=/tmp/bluebell-go-cache go test -race ./internal/store ./internal/service -run 'TestPostStore_SyncPosts\|TestOutbox_' -count=1` | 退出码 0；相关用例通过，无竞态报告 |
| 静态检查 | `GOCACHE=/tmp/bluebell-go-cache go vet ./...` | 退出码 0 |
| 补丁检查 | `git diff --check` | 退出码 0 |

## Outbox 批量优化 A/B（2026-10-11）

**本轮未验证到写链路可持续性的提升。** 发帖 HTTP p95 中位数仅降低 0.47%，投票增加 5.32%；后台等待峰值中位数分别增加 66.28% 和 19.90%。前后十二轮 HTTP 均达标，但后台均未通过原有标准。不能将这次结果写成吞吐提升或后台瓶颈已解决。

对照图表：[发帖六轮](./output/results/comparison-outbox-20261011-create_post.html)、[投票六轮](./output/results/comparison-outbox-20261011-vote.html)。[完整执行日志](./output/results/outbox-ab-20261011.log)、[执行脚本](./output/results/outbox-ab-20261011-run.sh)、[离线核验脚本](./output/results/outbox-ab-20261011-verify.py)、[核验与中位数 JSON](./output/results/outbox-ab-20261011-analysis.json)均保留。

### 版本、环境与负载

- A：直接基线 `3eb934a96aab29070a71050db62610ae46090c53`，4 路并发处理帖子任务；B：`20ef3c1683b851d60e41096317da75670a5f4118`，包含 `a35060d` 的批量 MySQL 查询、单帖 Lua 合并及 Redis Pipeline。两者每批最多 256 个事件，轮询间隔仍为 500ms；Outbox 确认删除代码没有变化。
- 沿用同一 WSL2/i9-13900HX/6 逻辑 CPU 环境与 Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。十二轮内存字段均为 4012868 KiB；历史报告为 4012864 KiB。本次前后条件完全一致，历史样本不参加百分比计算。
- MySQL 最大/空闲连接数均为 10，连接生命周期 3 分钟，info/JSON 日志；buffer pool 为 134217728 字节，`innodb_flush_log_at_trx_commit=1`、`sync_binlog=1`。准备与真实登录均在计时外，HTTP 超时、阈值与每两秒一次的 SQL 采样不变。
- 每轮恢复原 SQL 快照：200 用户、5 社区、2000 帖子、20000 评论、20000 投票；重建 Redis DB13。数据库仍为 `bluebell_k6_verify`，服务端口 18080，发帖正文 4 KiB。
- 发帖和投票均为 200 RPS、155 VU、10 秒升速、180 秒稳定施压，前后各三轮；依次运行发帖 A/B、投票 A/B。十二轮每轮稳定阶段均有 36000 次业务成功请求，实际成功速率均为 200.00 RPS，HTTP/业务失败率、丢弃和采样重试数均为 0。

数据准备工具已经迁移到 `SyncPosts`，旧测量指纹不能直接配对。本次只给脚本增加 `SERVER_SOURCE_DIR`，以同一套当前准备工具、配置、快照和 k6 脚本编译并测量两个版本；没有修改业务代码或调整服务配置。各轮真实提交号和二进制指纹均已核对。

| 指纹 | 值 |
| --- | --- |
| SQL 快照 | `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc` |
| 共同测量脚本 | `39e5229853ae8ac248a29a047ef8d45e4f7dd5a42f5d55b8a99c1a583754b56d` |
| A 服务二进制 | `310249e4d16487f51fbfd7ac8816acc5a99275601d6b257426a7572ae3f14d47` |
| B 服务二进制 | `e13a530314131390ebfe3965cdbf67612829386b31db8c86d6f7e8eb176a2087` |

本次准确执行方式如下；循环等价于四次 `make loadtest-repeat`，每次稳定施压 180 秒并重复三轮。退出码 99 表示阈值或后台未通过，继续保留全部三轮；其他错误立即停止。

```bash
git worktree add --detach /tmp/bluebell-outbox-before-3eb934a 3eb934a
export GOCACHE=/tmp/bluebell-go-cache RATE=200 VUS=155 RAMP_SECONDS=10
for endpoint in create_post vote; do
  for label in before after; do
    source_dir="$PWD"
    if [[ "$label" == before ]]; then source_dir=/tmp/bluebell-outbox-before-3eb934a; fi
    status=0
    ENDPOINT="$endpoint" LABEL="$label" SERVER_SOURCE_DIR="$source_dir" tests/load/run.sh repeat || status=$?
    (( status == 0 || status == 99 )) || exit "$status"
  done
done
```

### 全部正式轮次

以下峰值只取稳定施压阶段，采样数为 83–85 个。十二轮退出码文件均为 0；四组 `repeat` 的退出码均为 99，来自后台未通过。所有轮次停压后的最终待处理数均为 0。

| 接口 / 阶段 / 轮次 | p95 ms | p99 ms | 待处理峰值 | 最老等待峰值 秒 | 后台判断 | 原始报告 |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| 发帖 A1 | 27.97 | 75.60 | 6805 | 34.044 | 持续积压 | [查看](./output/results/20261011-151129927387655-before-create_post-200/report.html) |
| 发帖 A2 | 26.98 | 50.64 | 4510 | 22.561 | 需确认 | [查看](./output/results/20261011-151503052055508-before-create_post-200/report.html) |
| 发帖 A3 | 27.22 | 52.74 | 4063 | 20.327 | 需确认 | [查看](./output/results/20261011-151836535240767-before-create_post-200/report.html) |
| 发帖 B1 | 27.09 | 50.71 | 7501 | 37.514 | 持续积压 | [查看](./output/results/20261011-152211443914675-after-create_post-200/report.html) |
| 发帖 B2 | 27.13 | 52.38 | 6193 | 30.976 | 持续积压 | [查看](./output/results/20261011-152545322600201-after-create_post-200/report.html) |
| 发帖 B3 | 26.87 | 43.73 | 10087 | 50.443 | 需确认 | [查看](./output/results/20261011-152919641151740-after-create_post-200/report.html) |
| 投票 A1 | 25.42 | 30.48 | 10328 | 51.654 | 持续积压 | [查看](./output/results/20261011-153255084799017-before-vote-200/report.html) |
| 投票 A2 | 25.15 | 30.62 | 18741 | 93.719 | 持续积压 | [查看](./output/results/20261011-153629292138961-before-vote-200/report.html) |
| 投票 A3 | 26.89 | 38.26 | 12016 | 60.087 | 持续积压 | [查看](./output/results/20261011-154003897415650-before-vote-200/report.html) |
| 投票 B1 | 27.36 | 43.57 | 17175 | 85.885 | 持续积压 | [查看](./output/results/20261011-154339328590636-after-vote-200/report.html) |
| 投票 B2 | 26.10 | 32.86 | 13703 | 68.525 | 持续积压 | [查看](./output/results/20261011-154714052325081-after-vote-200/report.html) |
| 投票 B3 | 26.77 | 35.19 | 14407 | 72.047 | 持续积压 | [查看](./output/results/20261011-155048730265119-after-vote-200/report.html) |

### 三轮中位数对照

| 接口 / 指标 | A | B | 相对 A 的变化 |
| --- | ---: | ---: | --- |
| 发帖 p95 ms | 27.22 | 27.09 | 降低 0.47% |
| 发帖 p99 ms | 52.74 | 50.71 | 降低 3.84% |
| 发帖待处理峰值 | 4510 | 7501 | 增加 66.32% |
| 发帖最老等待峰值 秒 | 22.561 | 37.514 | 增加 66.28% |
| 投票 p95 ms | 25.42 | 26.77 | 增加 5.32% |
| 投票 p99 ms | 30.62 | 35.19 | 增加 14.95% |
| 投票待处理峰值 | 12016 | 14407 | 增加 19.90% |
| 投票最老等待峰值 秒 | 60.087 | 72.047 | 增加 19.90% |

固定负载下前后成功 RPS 均为 200，未验证吞吐提升。两组正式测量都未通过后台标准，因此没有继续提高负载。本次相对于直接基线的变化就是上述数值；2026-10-10 最初基线的脚本和环境字段不同，不另算累计收益。三轮波动较大，以上是本机样本的实测变化，不能仅凭这些峰值断言优化代码导致所有退化；0.47% 的 HTTP 延迟差也不足以证明稳定改善。

### 日志线索与剩余问题

`internal/repository/outbox.go:68` 的确认 DELETE 在保留日志中仍耗时很长。每轮保留样本中的最大耗时，发帖 A 为 31.065 / 19.633 / 19.218 秒、B 为 34.819 / 28.736 / 48.694 秒；投票 A 为 49.355 / 93.611 / 57.526 秒、B 为 81.555 / 67.088 / 72.041 秒。这是服务日志记录的调用耗时，包含数据库执行与等待，不能直接等同于纯 SQL 执行时间。诊断日志只保留最后 200 行，不是完整耗时分布。

批量查询和 Redis Pipeline 已落实到本轮 B 版本，但确认删除仍有长时间等待，应优先单独测量 `DeleteByIDs` 的执行计划、连接等待与锁等待。本次没有额外运行施压期间的 SQL 诊断或修改确认方式，尚不能判定具体根因。

### 验证与收尾

| 分类 | 命令 / 方法 | 结果 |
| --- | --- | --- |
| 脚本检查 | `bash -n tests/load/run.sh`、`git diff --check` | 退出码 0 |
| 冒烟 | `GOCACHE=/tmp/bluebell-go-cache make loadtest-smoke` | 六接口通过，错误和丢弃均为 0；[原始日志](./output/results/outbox-ab-20261011-smoke.log) |
| 正式测量 | 上述四组 `repeat` | 完成十二轮，无运行异常；每组退出码 99，后台均未通过 |
| 条件与台账核验 | `python3 tests/load/output/results/outbox-ab-20261011-verify.py` | 退出码 0；十二轮参数、环境、配置、实际版本、指纹、原始文件、稳定阶段峰值及后台判定逐项一致 |
| 对照报告 | 对每个接口分别执行 `make loadtest-compare`，`BEFORE/AFTER` 为上表对应三个目录 | 退出码 0；可比性校验通过，均显示优化后 0/3 轮通过；输出分别保存为上方两份对照 HTML |
| 台账 | 每轮自动刷新 `records.csv` | 新版 46 轮，包含本次 6 轮冒烟和 12 轮正式测量；历史 16 轮仍保留 |
| 数据恢复 | `GOCACHE=/tmp/bluebell-go-cache make loadtest-reset` | 退出码 0，恢复原五项数据规模并重建 Redis DB13；[恢复日志](./output/results/outbox-ab-20261011-reset.log) |
| 临时源码 | `git worktree remove /tmp/bluebell-outbox-before-3eb934a` | 退出码 0，主工作区仍为原分支；全部结果保留 |

本次只修改压测编排、记录与文档，没有重复执行业务 Go 回归测试；前一节的功能与竞态验证属于此前测试适配，不算本轮性能样本。原始 HTML/JSON/日志仍在被 Git 忽略的 output 目录，分享和迁移时需连同原始目录保留。

## Worker 分阶段诊断与确认删除验证（2026-10-11）

**当前限制 Worker 的环节是 Outbox 确认 DELETE：扫描本批 ID 集合之外的记录，并遇到这些记录上的锁等待。** 原版确认阶段占稳定窗口 98.81% / 98.97%；Redis 和投影查询各只占不到 0.5%。服务端绑定到具体 DELETE 的 `ROWS_EXAMINED`、实时主键锁等待和单变量 `LIMIT` 探针共同支持这个结论。实际执行计划类型未捕获，不推断优化器为什么选择该扫描方式。

[打开批次报告](./output/results/outbox-worker-20261011/report.html)；[分析 JSON](./output/results/outbox-worker-20261011/analysis.json)、[离线核验脚本](./output/results/outbox-worker-20261011/analyze.py)、[精确 SQL 与集合外锁证据](./output/results/outbox-worker-20261011/scan-lock-witnesses.json)均保留。原始逐批 `worker-profile.jsonl` 在各轮目录，报告另提供全部批次 CSV，包含每批事件数、合并任务数、尝试确认数、实际确认数、阶段耗时、事务耗时和错误。

### 条件与计量口径

沿用上一节的环境、数据快照、连接池、数据库参数和负载：200 RPS、155 VU、10 秒升速、180 秒稳定阶段；每轮恢复 200/5/2000/20000/20000 数据规模、Redis DB13 和端口 18080。`20ef3c1` 在隔离 worktree 中添加临时计时；前两轮不改变处理逻辑，第三轮仅在 `DeleteByIDs` 增加 `.Limit(len(ids))`。三个摘要中的配置、数据、环境、快照和测量脚本指纹完全一致，带诊断的二进制不同于正式 A/B，不混算收益。

- 实际确认吞吐 = 稳定窗口内成功 COMMIT 后的 DELETE 实际 `RowsAffected` 总和 / 180 秒。窗口使用摘要的 `load_start_ms`，不是服务启动时间；可能包含升速阶段到达、稳定阶段才完成的事件。
- 批次分布统计稳定窗口内提交成功的批次，使用完整批次单调时钟耗时。阶段占用则将全部批次的起止时间裁剪到稳定窗口，包含窗口末尾尚未确认的批次；两个口径不能混加。三轮均有跨起点、跨终点各一批。
- 事务开始、SQL、提交使用 GORM 对应回调独立计时；SQL 调用包含服务端执行与等待。实际确认只在提交成功后计数。共享 `database/sql` 等待计数包括 API，不能当作 Worker 自己的等待时间。
- 分位数按 `(n-1)×q` 线性插值。单调时间与墙钟区间最大差异为 3.265ms，墙钟只用于窗口归属/裁剪，不替代批次耗时。

三轮稳定阶段均有 36000 次业务成功请求，HTTP / 业务错误、丢弃均为 0；每轮整个升速、稳定和收尾阶段共实际确认 37004 个事件，尝试确认数一致，无重试、无批次错误。

### 实际确认吞吐与每批耗时

| 场景 | 稳定窗口实际确认数 | 实际确认 事件/秒 | 提交成功批次数 | 每批 p50 / p95 / p99 / 最大 ms | 积压峰值 / 最老等待秒 | 后台判断 |
| --- | ---: | ---: | ---: | --- | --- | --- |
| [原版发帖](./output/results/20261011-161614643466545-diagnostic-create_post-200/report.html) | 32227 | 179.039 | 567 | 22.473 / 43.576 / 7809.170 / 20806.048 | 4387 / 21.949 | 持续积压 |
| [原版投票](./output/results/20261011-161949712195041-diagnostic-vote-200/report.html) | 33252 | 184.733 | 290 | 26.275 / 40.399 / 19301.352 / 57350.503 | 11469 / 57.368 | 需确认（未通过） |
| [投票 LIMIT 探针](./output/results/20261011-163004290117747-diagnostic_limit-vote-200/report.html) | 36003 | 200.017 | 7822 | 22.255 / 29.754 / 34.640 / 133.388 | 12 / 0.076 | 未见持续积压 |

原版满 256 事件批次分别有 118、127 批；其余批次小得多。只看中位数或 p95 会遗漏少量秒级 DELETE，p99 与最大值更能解释此负载下的确认缺口。LIMIT 轮多确认的 3 个事件来自窗口边界，不能写成负载上限超过 200 RPS；单轮诊断也不能替代正式三轮容量验证。

### 阶段占用与事务拆分

以下按稳定 180 秒窗口裁剪，单位为秒；确认阶段包含事务开始、SQL 调用和提交。

| 阶段 | 原版发帖 | 原版投票 | LIMIT 投票 |
| --- | ---: | ---: | ---: |
| 获取到期事件 | 0.764 | 0.489 | 7.078 |
| 合并及准备任务 | 0.009 | 0.029 | 0.084 |
| 查询帖子 | 0.665 | 0.361 | 6.168 |
| 查询投票 | 0 | 0.465 | 6.274 |
| Redis 批量同步 | 0.684 | 0.487 | 4.133 |
| 重试及收集确认 ID | 0.001 | 0.001 | 0.004 |
| 确认删除 | 177.852（98.807%） | 178.150（98.972%） | 155.891（86.606%） |

以下仅统计稳定窗口内提交成功的批次，累计秒数不等于上表裁剪后的阶段占用。

| 确认事务阶段 | 原版发帖 累计秒 / 最大 ms | 原版投票 累计秒 / 最大 ms | LIMIT 投票 累计秒 / 最大 ms |
| --- | --- | --- | --- |
| BEGIN | 0.247 / 14.198 | 0.116 / 10.774 | 2.812 / 20.842 |
| DELETE SQL 调用 | 152.093 / 20783.124 | 162.217 / 57339.186 | 5.817 / 26.065 |
| COMMIT | 10.771 / 36.583 | 5.436 / 46.222 | 147.172 / 86.918 |

原版的秒级长尾落在 DELETE 调用，不在连接获取、Redis 或 COMMIT。共享池在相交批次内也记录了等待（原版发帖 994 次 / 25.552 秒、投票 419 次 / 16.460 秒），但这是全应用计数；BEGIN 最大仅 14.198ms，不能把整段秒级 DELETE 归因于连接池排队。LIMIT 消除该长尾后，每批确认仍要提交；7822 批平均每批 4.60 个事件，COMMIT 累计 147.172 秒，成为该探针下最大的剩余开销。本轮未调整持久化参数，也未验证批次聚合或更高负载。

### 服务端扫描与锁证据

预处理执行在 `performance_schema` 中记为 `statement/com/Execute`，`SQL_TEXT` 为 NULL。通过运行中的 `PROCESSLIST_INFO` 与 `(THREAD_ID, EVENT_ID)` 绑定执行记录；256 ID 语句被截断时，再匹配保留的 GORM 完整 SQL 还原 ID 集合。共精确绑定 7 条慢 DELETE，不把其他无法绑定的 Execute 当作确定的 DELETE。

| 本批目标 ID | 实际删除 | 检查行数 | 服务端耗时 秒 | 实时观察到的集合外 PRIMARY 锁 ID |
| --- | ---: | ---: | ---: | --- |
| 3128–3138 | 11 | 11480 | 57.335 | 此语句未捕获相应锁样本 |
| 13635–13890 | 256 | 6981 | 28.783 | 15080、17473、17969、20132 |
| 20291–20546 | 256 | 4122 | 18.104 | 21550 |
| 23875–24130 | 256 | 9684 | 45.224 | 28278、28748 |
| 33347–33602 | 256 | 1225 | 3.870 | 未捕获 |
| 34115–34370 | 256 | 2170 | 8.526 | 34899 |
| 35651–35906 | 256 | 2760 | 10.401 | 36832、37778 |

这些锁均在 `outbox_events.PRIMARY` 上：DELETE 请求 `X`，阻塞记录为 `X,REC_NOT_GAP`。锁 ID 超过本批最大 ID，说明删除扫描/锁定范围超出了待确认集合。记录中的请求/阻塞线程字段有同值情况，未据此推断具体阻塞连接或事务身份；准确优化器计划也未捕获。慢样本 `NO_INDEX_USED=0`，不能写成“完全没用索引”。

投票验证仅将 SQL 改为 `DELETE FROM outbox_events WHERE id IN (...) LIMIT 本批数量`，仍保留提交与持久化配置。实际 DELETE 调用最大从原版 57.339 秒降为 26.065ms，后台检查通过，支持限制扫描范围的方向。校准服务端计时并排除历史残留后，7797 条疑似批量 DELETE 的预处理样本检查行数/影响行数最大为 1、服务端最大耗时 25.081ms；因这些样本 `SQL_TEXT=NULL`，只作为辅助证据，确定的确认数以 Go 提交计时为准。LIMIT 的具体计划与生产稳健性仍需后续正式对照，不在本轮直接合入。

### 复现、核验与收尾

临时源码来自 `git worktree add --detach /tmp/bluebell-outbox-profile 20ef3c1`，计时补丁见 [instrumentation.patch](./output/results/outbox-worker-20261011/instrumentation.patch)，LIMIT 版本见 [limit-probe.patch](./output/results/outbox-worker-20261011/limit-probe.patch)，两份补丁任选其一。诊断期间在 `run.sh` 过滤日志前临时导出完整批次，导出补丁另存为 [trace-export.patch](./output/results/outbox-worker-20261011/trace-export.patch)，收尾已撤销此导出。

```bash
# 在隔离源码应用对应补丁，并在日志过滤前导出完整批次后执行：
GOCACHE=/tmp/bluebell-go-cache SERVER_SOURCE_DIR=/tmp/bluebell-outbox-profile \
  ENDPOINT=create_post RATE=200 VUS=155 RAMP_SECONDS=10 HOLD_SECONDS=180 LABEL=diagnostic tests/load/run.sh run
# 同参数再测 ENDPOINT=vote；LIMIT 版本再测 ENDPOINT=vote LABEL=diagnostic_limit。
python3 tests/load/output/results/outbox-worker-20261011/analyze.py
```

[原版驱动日志](./output/results/outbox-worker-20261011/driver.log)、[LIMIT 驱动日志](./output/results/outbox-worker-20261011/limit-driver.log)、驱动脚本和 MySQL 原始采样保存在同一目录。原版两轮编排返回 99（后台未通过），LIMIT 返回 0；三个 k6 退出码均为 0。首次服务端观察器未覆盖 prepared Execute，纠正后的 v2 只覆盖原版投票后半段，LIMIT 全轮使用 v2；因此不宣称拿到了原版全部 SQL 分布。首次 LIMIT 测试构造了重复 LIMIT、SQL 报错，修正并通过 SQL 断言后才运行验证压测；失败日志也保留，未计入性能样本。

| 分类 | 命令 / 方法 | 结果与限制 |
| --- | --- | --- |
| 原版插桩校验 | `go test ./internal/service -run '^TestOutbox_BatchQueriesAndBatchAck$' -count=1` | 通过，37 事件合并 9 任务并实际确认 37；计时回调顺序已验证 |
| LIMIT SQL 校验 | 隔离 worktree 的 `go test ./internal/repository -run TestDiagnosticDeleteLimitSQL -count=1` | 通过，断言 SQL 只有一个 LIMIT；测试源码同目录保存为 `outbox_profile_test.go` |
| Outbox 回归 | 隔离 worktree 的 `go test ./internal/service -run 'TestOutbox_' -count=1` | 普通批次、部分失败、取消、重试、257 条分批通过；帖子缺失子用例失败 |
| 既有失败复核 | 未修改业务代码的主工作区：`GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run 'TestOutbox_MySQLBatchFailurePreservesEvents/帖子缺失$' -count=1 -v` | 同样失败：未保留/重试该事件；[日志](./output/results/outbox-worker-20261011/main-missing-post-test.log)。当前 `syncTasks` 缺少帖子数量完整性校验，本轮未修复；压测快照不触发帖子缺失 |
| 离线核验 | 上述 `analyze.py` | 三轮参数、窗口、确认数、阶段计时与跨窗口占用断言通过；导出全部批次 CSV 和 JSON |
| 台账 | 每轮自动刷新 `records.csv` | 新版 49 轮，保留此前 46 轮，增加 2 轮诊断和 1 轮 LIMIT 探针；历史 16 轮不变 |
| 收尾 | 撤销主脚本临时导出、移除隔离 worktree、`make loadtest-reset` | 保留补丁和结果；恢复原数据与 Redis DB13，MySQL 两项临时采样 consumer 恢复 NO，端口 18080 无遗留服务 |

本轮业务代码与配置未改，新增的是诊断结果、台账和说明。证据支持先处理确认 DELETE 的扫描/锁范围；帖子缺失的既有正确性失败需另外修复。正式三轮 A/B 结果仍保留上一节，单轮 LIMIT 探针不算已交付优化或生产容量提升。结果目录被 Git 忽略，迁移时需复制该诊断目录及三个原始轮次。

## DELETE 实际计划与阻塞事务追踪（2026-10-11）

**进一步确认：优化器能识别 PRIMARY 等值范围，却会因估算成本主动放弃该范围访问；原 DELETE 随后扫描到新插入的 Outbox 记录，等待投票事务持有的锁。** 这补齐了上一节尚未捕获的执行计划和事务身份。另一个新边界是：`LIMIT len(ids)` 在部分目标不存在时仍可能扫描并锁等待，不能作为保证精准访问的最终修复。

[诊断报告](./output/results/outbox-plan-20261011/report.html)、[完整分析](./output/results/outbox-plan-20261011/analysis.json)、[509 条计划 CSV](./output/results/outbox-plan-20261011/plans.csv)、[扫描计划与集合外锁证据](./output/results/outbox-plan-20261011/plan-lock-witnesses.json)、[离线核验脚本](./output/results/outbox-plan-20261011/analyze.py)保留在新目录，上一节结果未覆盖。

### 本轮条件与采集方法

原源码仍为隔离的 `20ef3c1`，原 DELETE 未添加 LIMIT。环境、数据库持久化参数、连接池、数据规模与快照保持上一节条件；投票 200 RPS、155 VU、10 秒升速、180 秒稳定阶段。新增加的会话 SET、trace 读取、事件日志和 200ms 观察器具有诊断开销，本轮不与正式 A/B 计算收益。

计时回调在 GORM 已开始的 DELETE 事务上，通过 `tx.Statement.ConnPool` 开启会话 trace，原预处理 DELETE 执行完毕后、COMMIT 之前读取 `information_schema.OPTIMIZER_TRACE`，随后关闭 trace。记录实际连接 ID、参数类型、隔离级别、范围优化内存和 index dive 阈值；SQL 耗时单独计量，trace 的设置/读取开销另外记录。原客户端的 prepared 执行方式保持不变，没有用另一个连接上的字面量 EXPLAIN 代替。

只读观察器保存 `data_lock_waits` 的请求/阻塞 `ENGINE_TRANSACTION_ID` 和锁 ID，再关联 `INNODB_TRX.TRX_MYSQL_THREAD_ID`、线程和执行状态；锁详情同时按引擎、锁 ID、事务 ID 关联。业务端记录刚插入的事件 ID、类型和帖子 ID，便于绑定集合外锁对应的事件。会话 trace 和锁行为说明分别参考 [MySQL trace 文档](https://dev.mysql.com/doc/refman/8.4/en/optimizer-tracing-typical-usage.html)、[锁等待事务字段](https://dev.mysql.com/doc/refman/8.4/en/performance-schema-data-lock-waits-table.html)。

### 实际预处理计划为什么放弃主键范围

在 [本轮原始轮次](./output/results/20261011-182640492240623-plan_trace-vote-200/report.html) 中完整采集了 509 条 DELETE：

- 496 条选择 PRIMARY 等值范围；13 条明确标记 `chosen: false, cause: "cost"`。
- 13 条均能解析出目标 ID 的主键等值范围，参数均为 `LONGLONG`；没有 trace 截断、权限缺失或采集错误。不是索引不存在，也不是参数导致完全无法识别范围。
- 所有 6 条超过 1 秒的 DELETE 都属于因成本拒绝范围访问的 13 条；最长调用 50.770 秒。其余成本拒绝样本可能很快完成，宽扫描遇到并发持锁才产生本轮长尾。

一个容易核对的例子是 `DELETE ... WHERE id IN (2504,...,2523)`：

| trace / 运行指标 | 值 |
| --- | --- |
| PRIMARY 范围 | 20 个 `id = 常量`，可用 |
| 主键范围估算行数 / 成本 | 20 / 7.01871 |
| 表扫描估算行数 / 成本 | 24 / 4.75 |
| 范围访问选择 | `chosen: false, cause: "cost"` |
| 服务端实际检查 / 删除行数 | 276 / 20 |
| Go DELETE 调用耗时 | 1271.291ms |

成本是优化器内部数值，不是毫秒。优化器当时认为扫描约 24 行比 20 次等值范围访问更便宜；执行期间访问到后续新事件，实际检查了 276 行并等待锁。此前“小表或高命中比例促使优化器选择扫描”的假设，现已获得真实会话 trace 支持。不能把这解释成“所有 IN 都不会使用主键”或“肯定是统计信息错误”；本轮多数 DELETE 正常使用主键范围，已捕获的拒绝理由就是 `cost`。[MySQL 对扫描成本选择的说明](https://dev.mysql.com/doc/refman/8.4/en/table-scan-avoidance.html)

### 具体阻塞事务与投票事件

上面同一条 20 ID DELETE 捕获了如下等待关系：

| 对象 | 事务 ID | MySQL 连接 ID | 记录 / 状态 |
| --- | --- | --- | --- |
| Worker DELETE | 1429806 | 2267 | `LOCK WAIT`，目标 2504–2523，却请求 PRIMARY `id=2533` 的 X 锁 |
| 阻塞方 | 1429811 | 2261 | 持有 `id=2533` 的 `X,REC_NOT_GAP`，执行采样显示 `COMMIT` |
| 刚插入的事件 | — | — | `id=2533`，`post.vote.sync`，帖子 83；业务日志与锁 ID 对应 |

`PostRepository.Vote` 在同一业务事务中写投票并插入 Outbox，直到事务提交才释放该新记录的锁。Worker 的扫描遇到了这笔未完成事务写入的记录。锁竞争在 `outbox_events.PRIMARY`，不是 `posts` 的 SHARE 锁或 `post_votes` 的 Upsert 锁；锁对象可直接区分。[InnoDB DELETE / INSERT 锁行为](https://dev.mysql.com/doc/refman/8.4/en/innodb-locks-set.html)

原始观察器共采集 173 条锁样本，163 条关联执行记录显示 COMMIT；8 条显示 trace SELECT，2 条缺少阻塞 SQL。锁视图、INNODB_TRX 和执行视图不是原子快照，连接在事务结束后可能很快复用，因此后 10 条不用于判断阻塞方执行阶段；这些数字是采样条数，不是独立事务数或等待时间。

旧样本里请求/阻塞线程字段相同的现象再次出现。新采样的独立引擎事务 ID 与 INNODB_TRX 连接能区分双方；还通过下面的可控实验固定实际 writer 身份，避免仅凭非原子执行状态下结论。

### 秒级可控两事务复现与 LIMIT 边界

[复现源码](./output/results/outbox-plan-20261011/repro.go)直接调用真实 `PostRepository.Vote`。在独立测试库先插入 20 条已提交的目标事件；Vote 再插入一个新事件，Create 回调在同一事务上读出连接 ID，并暂缓提交。另一个连接执行原 GORM DELETE；观察器将阻塞事务、连接和记录 ID 与被固定的 Vote 调用绑定，捕获等待后才释放 writer。

该实验复写已有投票的原方向，清理范围只包含工具创建的事件；不运行 Redis 投影，不算 k6 性能轮次。每组三个变体不到 1 秒，连续三次得到同一结果：[第一次](./output/results/outbox-plan-20261011/repro.log)、[第二次](./output/results/outbox-plan-20261011/repro-repeat-2.log)、[第三次](./output/results/outbox-plan-20261011/repro-repeat-3.log)。

| 变体 | 目标 / 实际删除 | 等待新事件上的投票事务锁 | 第一次实际计划 |
| --- | --- | --- | --- |
| 原 DELETE | 20 / 20 | 是 | 因成本拒绝 PRIMARY 范围 |
| 增加 LIMIT 20 | 20 / 20 | 否 | 选择 PRIMARY 范围 |
| LIMIT 20，但其中一个目标已不存在 | 20 / 19 | 是 | 再次因成本拒绝 PRIMARY 范围 |

第一组原 DELETE 的目标为 38411–38430，实际 writer 写入 38431：

- Vote 的事务 ID **1468500**、连接 **2363**，由实际 Vote 回调固定；它持有 38431 的排他记录锁。
- DELETE 的事务 ID **1468501**、连接 **2365**，请求同一记录的 X 锁；38431 不在目标列表中。
- 当时 trace 估算扫描 21 行成本 **4.45**，主键范围成本 **7.01871**，以 `cost` 拒绝范围访问。释放 Vote 后 DELETE 才完成。

这个实验确定了实际持锁者是 Vote 的插入事务，补强压力轮的身份关联。其 writer 被人为暂停在提交之前，因此该实验的延迟不是正常 COMMIT 延迟分布。

LIMIT 缺失目标的反例很关键：只有 19 条可删除，无法达到 LIMIT 20 的结束条件，而执行计划仍可能选择扫描，从而碰到后面的新事件锁。以后不能仅凭正常快照的一轮 LIMIT 通过就合入；应验证实际计划稳定的主键访问方案，同时覆盖非连续 ID、缺失 ID、并发写入和重放。

### 本轮验证、范围与收尾

| 项目 | 结果与限制 |
| --- | --- |
| 原有单批集成测试 | 隔离 worktree：`GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run '^TestOutbox_BatchQueriesAndBatchAck$' -count=1 -v` 通过，实际确认 37 / 合并 9 任务，trace 无错误；[日志](./output/results/outbox-plan-20261011/instrumentation-test.log) |
| 当前帖子缺失用例 | 用户在主工作区恢复完整性校验后，`GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run 'TestOutbox_MySQLBatchFailurePreservesEvents/帖子缺失$' -count=1 -v` 通过（0.738 秒）；[日志](./output/results/outbox-plan-20261011/current-missing-post-test.log)。前文失败属于旧版本，不代表当前校验仍失败 |
| 原负载诊断 | [驱动脚本](./output/results/outbox-plan-20261011/run.sh)与[日志](./output/results/outbox-plan-20261011/driver.log)；200 RPS / 155 VU / 10+180 秒，36000 次稳定业务成功，p95 26.795ms，HTTP / 业务失败与丢弃均为 0；编排返回 99，后台未通过 |
| 确认计数 | 全轮实际确认 37004，无事件重试和批次错误；稳定窗口确认 29053，即 161.406 事件/秒。会话采集有额外开销，此数字只用于本轮诊断，不与前一轮计算退化或收益 |
| 可控复现 | 原版 / LIMIT / LIMIT 缺失目标三个变体，重复三次均通过预期断言；运行命令、环境与编译路径见下面说明 |
| 离线核验 | `python3 tests/load/output/results/outbox-plan-20261011/analyze.py` 通过；509 条真实计划与批次逐一绑定，确认 ID 无重复，13 条成本拒绝、6 条秒级 DELETE、锁记录与事件以及九个可控用例逐项断言 |
| 台账 | 新版 50 轮，保留此前 49 轮，仅新增本轮 `plan_trace`；九个可控用例不写入性能台账 |
| 收尾 | 临时 worktree 删除、采样 consumer 恢复原值、测试库快照及 Redis DB13 恢复、18080 无遗留服务；主脚本本轮没有修改 |

诊断源码补丁另存 [instrumentation.patch](./output/results/outbox-plan-20261011/instrumentation.patch)。压力编排使用归档的 `load-run.sh` 副本导出三类日志，摘要记录该副本的真实测量脚本指纹；没有在主 `run.sh` 留下导出代码。复现时在隔离 worktree 应用补丁，将 `repro.go` 复制到临时 `cmd/outbox-plan-repro/main.go` 后编译；设置 `BLUEBELL_CONFIG_FILE` 为当前压测配置，通过环境变量 `OUTBOX_DIAG_ROOT_PASSWORD` 提供本机测试实例 root 密码，再运行工具。该凭据不保存到日志或源码。压力和工具输出均包含诊断开销，不用作生产容量承诺。

主工作区用户暂存的 `internal/service/post.go` 辅助函数移动与 `internal/worker/outbox.go` 帖子完整性校验均已保留，诊断工作没有修改这些业务代码。恢复校验后的帖子缺失用例已复核通过；本轮没有对用户改动运行全量 Go 回归。压力与可控复现仍对应隔离的 `20ef3c1`，不包含后来恢复的校验。结果修正了“LIMIT 足以稳定限制扫描范围”的潜在理解；后续优化应验证精准主键访问和提交批次聚合。

## Outbox 删除优化复测（2026-10-11）

**200 RPS 下，优化版发帖与投票各三轮均通过 HTTP 和后台标准。** 相对直接基线 `20ef3c1`，稳定阶段最老事件等待峰值的三轮中位数：发帖由 37.514 秒降至 0.447 秒，降低 98.81%；投票由 72.047 秒降至 0.075 秒，降低 99.90%。HTTP p95 中位数分别增加 8.73% 和 2.70%，本轮没有验证到 HTTP 延迟改善或更高的吞吐上限。

直接对照：[发帖六轮](./output/results/comparison-outbox-delete-20261011-direct-create_post.html)、[投票六轮](./output/results/comparison-outbox-delete-20261011-direct-vote.html)。[分析 JSON](./output/results/outbox-delete-20261011-analysis.json)、[离线核验脚本](./output/results/outbox-delete-20261011-verify.py)、[核验日志](./output/results/outbox-delete-20261011-verify.log)、[对照命令与日志](./output/results/outbox-delete-20261011-compare.log)均保留。

### 版本与复用依据

- 直接基线为上一次批量优化 A/B 中 `20ef3c1683b851d60e41096317da75670a5f4118` 的六轮正式 `after`；按用户要求直接复用，不使用 Worker 插桩、LIMIT 或 optimizer trace 的诊断轮次。本次对照中的“优化前”是比较阶段，原始摘要与台账仍保留旧 `after` 标签。
- 优化版为 `cf6c53e71b17899a84b60d2ac9c4e422b837cdb8`：确认删除改为带 PRIMARY 索引提示的 `Exec`，同时恢复帖子数量完整性校验。测试维护没有修改业务代码。这是整个版本的对照，包含 SQL 提示、执行路径与完整性校验的变化，没有逐项拆分收益。
- 两接口各 200 RPS、155 VU、10 秒升速、180 秒稳定施压，各三轮。每轮恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票；独立 MySQL `bluebell_k6_verify`、Redis DB13、端口 18080，发帖正文 4 KiB。
- 复用基线与新增六轮的配置、环境、负载、阈值、数据规模、快照及脚本指纹逐项一致。测量脚本指纹为 `39e5229853ae8ac248a29a047ef8d45e4f7dd5a42f5d55b8a99c1a583754b56d`，当时的编排脚本另存 [load-run.sh](./output/results/outbox-delete-20261011-load-run.sh)。同一版本的各轮服务程序指纹一致；离线报告排版不参加测量脚本指纹。
- 环境仍为 WSL2、i9-13900HX、6 逻辑 CPU、4012868 KiB 内存；Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。MySQL 最大/空闲连接数均为 10，生命周期 3 分钟，info/JSON 日志；详细条件保留在各轮摘要和分析 JSON。

优化版命令均为：

```bash
GOCACHE=/tmp/bluebell-go-cache ENDPOINT=vote RATE=200 VUS=155 RAMP_SECONDS=10 LABEL=after tests/load/run.sh repeat
GOCACHE=/tmp/bluebell-go-cache ENDPOINT=create_post RATE=200 VUS=155 RAMP_SECONDS=10 LABEL=after tests/load/run.sh repeat
```

投票通过 [原始驱动脚本](./output/results/outbox-delete-20261011-original-run.sh) 启动，日志见 [投票及停止记录](./output/results/outbox-delete-20261011.log)；发帖随后单独执行，见 [发帖日志](./output/results/outbox-delete-20261011-create_post.log)。原驱动曾在投票优化后三轮结束后启动旧版复测；用户要求停止时，一轮已完成、下一轮已开始。已终止专用进程组，不再运行旧版；完成的额外轮次 `20261011-201758880688380-before-vote-200` 保留在台账但不纳入收益计算，被中断的 `20261011-202132756121929-before-vote-200` 保留原始目录和部分摘要，缺少单轮退出码，不能作为完整性能样本；驱动退出码 143。

### 每轮结果

下表稳定阶段 HTTP / 业务失败率、丢弃与采样重试均为 0。每轮成功请求数为 36000，只有优化版投票第二轮为 36001（200.006 RPS），不据此计算吞吐提升。后台判定沿用每两秒采样、等待始终 ≤ 1 秒等原有标准；“需确认”也算未通过。

| 接口 / 阶段 | 原始轮次与报告 | p95 ms | p99 ms | 积压峰值 / 最老等待秒 | 后台判定 |
| --- | --- | ---: | ---: | --- | --- |
| 发帖 / 复用基线 1 | [152211443914675](./output/results/20261011-152211443914675-after-create_post-200/report.html) | 27.093 | 50.711 | 7501 / 37.514 | 持续积压 |
| 发帖 / 复用基线 2 | [152545322600201](./output/results/20261011-152545322600201-after-create_post-200/report.html) | 27.131 | 52.376 | 6193 / 30.976 | 持续积压 |
| 发帖 / 复用基线 3 | [152919641151740](./output/results/20261011-152919641151740-after-create_post-200/report.html) | 26.869 | 43.726 | 10087 / 50.443 | 需确认 |
| 发帖 / 优化后 1 | [202245641256597](./output/results/20261011-202245641256597-after-create_post-200/report.html) | 29.457 | 52.786 | 12 / 0.156 | 未见持续积压 |
| 发帖 / 优化后 2 | [202619724055722](./output/results/20261011-202619724055722-after-create_post-200/report.html) | 29.816 | 52.982 | 72 / 0.447 | 未见持续积压 |
| 发帖 / 优化后 3 | [202954185922046](./output/results/20261011-202954185922046-after-create_post-200/report.html) | 29.427 | 53.705 | 98 / 0.602 | 未见持续积压 |
| 投票 / 复用基线 1 | [154339328590636](./output/results/20261011-154339328590636-after-vote-200/report.html) | 27.363 | 43.571 | 17175 / 85.885 | 持续积压 |
| 投票 / 复用基线 2 | [154714052325081](./output/results/20261011-154714052325081-after-vote-200/report.html) | 26.098 | 32.855 | 13703 / 68.525 | 持续积压 |
| 投票 / 复用基线 3 | [155048730265119](./output/results/20261011-155048730265119-after-vote-200/report.html) | 26.769 | 35.193 | 14407 / 72.047 | 持续积压 |
| 投票 / 优化后 1 | [200713567767448](./output/results/20261011-200713567767448-after-vote-200/report.html) | 27.785 | 35.538 | 11 / 0.067 | 未见持续积压 |
| 投票 / 优化后 2 | [201048230206669](./output/results/20261011-201048230206669-after-vote-200/report.html) | 27.492 | 33.736 | 11 / 0.076 | 未见持续积压 |
| 投票 / 优化后 3 | [201422029802284](./output/results/20261011-201422029802284-after-vote-200/report.html) | 27.190 | 34.942 | 10 / 0.075 | 未见持续积压 |

### 相对直接基线的收益

以下各项分别取三轮中位数；积压数量与等待年龄都是稳定阶段采样峰值，不是逐条事件耗时的分位数。

| 指标 | 发帖：优化前 → 优化后 | 投票：优化前 → 优化后 |
| --- | --- | --- |
| HTTP p95 | 27.093 → 29.457ms，增加 8.73% | 26.769 → 27.492ms，增加 2.70% |
| HTTP p99 | 50.711 → 52.982ms，增加 4.48% | 35.193 → 34.942ms，降低 0.71% |
| 后台待处理峰值 | 7501 → 72 条，降低 99.04% | 14407 → 11 条，降低 99.92% |
| 最老事件等待峰值 | 37.514 → 0.447 秒，降低 98.81% | 72.047 → 0.075 秒，降低 99.90% |
| 成功请求速率 | 200.00 → 200.00 RPS | 200.00 → 200.00 RPS |
| HTTP + 后台通过轮数 | 0/3 → 3/3 | 0/3 → 3/3 |

HTTP p95 绝对值分别增加 2.364ms、0.723ms，仍远低于 500ms 阈值；本轮未对增加原因做单变量诊断，不推断具体原因。主要收益体现在原有负载下后台等待与积压的下降。保留的慢 SQL 日志中，优化版仍出现 252.833–328.573ms 的 DELETE，说明没有消除所有确认开销；日志经过截断，不作为完整 SQL 耗时分布或精确最大值。

### 相对最初基线的累计变化

额外复用 `3eb934a` 的六轮正式基线，条件同样逐项一致；分别直接计算最终版本相对最初版本的变化，不累加两次优化百分比。[累计发帖对照](./output/results/comparison-outbox-delete-20261011-cumulative-create_post.html)、[累计投票对照](./output/results/comparison-outbox-delete-20261011-cumulative-vote.html)及原始轮次名单保存在分析 JSON。

| 接口 | HTTP p95 中位数 | 后台峰值中位数 | 等待峰值中位数 |
| --- | --- | --- | --- |
| 发帖 | 27.221 → 29.457ms，增加 8.22% | 4510 → 72 条，降低 98.40% | 22.561 → 0.447 秒，降低 98.02% |
| 投票 | 25.417 → 27.492ms，增加 8.16% | 12016 → 11 条，降低 99.91% | 60.087 → 0.075 秒，降低 99.88% |

### 测试维护与验证

补充删除边界测试：nil / 空 ID、不存在与重复 ID、256 条精准删除且保留集合外事件；验证取消不删除、显式事务回滚；持有集合外新事件的插入事务，分别验证目标完整与目标缺失时确认都能完成并保留新事件。批量 Worker 用例同时断言一次确认调用实际删除全部 37 条通知。既有帖子 / 投票缺失、整批查询失败、取消、部分失败与恢复用例继续覆盖完整性校验和重试行为。

离线报告改为按 `BEFORE` / `AFTER` 输入分组，允许复用历史 `after`；拒绝 2/4 等错误分组，继续拒绝不可比数据的收益计算。报告分别展示优化前后通过轮数，优化前失败不会再遮蔽优化后三轮通过的结论；原始摘要及台账标签不改写。

| 验证范围 | 命令 / 方法 | 结果与证据 |
| --- | --- | --- |
| 相关功能回归 | `GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run '^(TestOutbox_.*\|TestPost_CreateAndDeleteSyncRanking\|TestPost_VoteSyncsLatestState)$' -count=1 -v` | 退出码 0，10.185 秒；[日志](./output/results/outbox-delete-20261011-tests.log) |
| Outbox 竞态 | `GOCACHE=/tmp/bluebell-go-cache go test -race ./internal/service -run '^TestOutbox_' -count=1 -v` | 退出码 0，11.174 秒，无竞态报告；[日志](./output/results/outbox-delete-20261011-race.log) |
| 相关包静态检查 | `GOCACHE=/tmp/bluebell-go-cache go vet ./internal/repository ./internal/worker ./internal/service` | 退出码 0；[日志](./output/results/outbox-delete-20261011-vet.log) |
| 数据核验 | 上述离线核验脚本 | 退出码 0；复用基线、新增测量的条件、稳定阶段采样、后台判定与台账逐项一致；累计对照另核验最初六轮 |
| 原生 HTML 报告 | `make loadtest-compare`，准确参数见对照日志 | 四份直接 / 累计报告均通过可比性检查，优化后均显示 3/3 通过 |
| 报告边界 | 原始 after 作为 BEFORE；2/4 错误分组；模拟脚本指纹不一致 | 全部按预期；[校验日志](./output/results/outbox-delete-20261011-report-check.log) |
| 数据与进程恢复 | `GOCACHE=/tmp/bluebell-go-cache make loadtest-reset`，随后核验 SQL 数量与监听端口 | 退出码 0；200 / 5 / 2000 / 20000 / 20000，Outbox 为 0，18080 无监听；[恢复日志](./output/results/outbox-delete-20261011-reset.log) |
| 清理与补丁检查 | 删除本轮临时旧版源码及报告校验夹具；`git diff --check` | 通过；原工作区已有改动与所有原始结果保留 |

完整验证状态见 [validation.json](./output/results/outbox-delete-20261011-validation.json)。新版台账共 57 轮：保留原 50 轮，增加六轮优化版正式测量及一轮被排除出对照的旧版完整测量；中断轮次在上文单独记录，没有混入性能台账。

按用户要求没有执行全量测试，也未追加更高 RPS 的容量探测。可以引用：“相同本机环境下，发帖与投票分别以 200 RPS 连续施压 3 分钟、重复 3 轮，优化后六轮均无请求失败、丢弃或持续后台积压；最老待处理事件等待峰值的三轮中位数分别降低 98.81% 和 99.90%。”

结果只验证该负载与时长；基线复用当日较早结果，没有交错运行或单独控制两次测试间的缓存、后台负载差异。两秒采样可能漏掉短暂峰值，等待年龄不等于单条事件完成耗时，HTTP 成功数不等于 Worker 实际提交确认数，也没有测量最大吞吐。测试没有临时业务插桩；原始 JSON、HTML、日志和曲线在 Git 忽略的 output 目录，分享或迁移时需连同相关目录保留。
