# 当前压测发现

最新定位见 [评论列表查询计划与连接池诊断](#评论列表查询计划与连接池诊断2026-10-12)：评论 COUNT/分页的索引交集扫描放大，引发共享连接池排队；两轮索引探针 p95 为 24.91/30.46ms，回退原计划后再现 1026.68ms。正式业务与索引尚未修改。

容量结果见 [动态 VU 校准与混合负载复测](#动态-vu-校准与混合负载复测2026-10-12)：800 RPS /10 分钟、907 VU 通过，零丢弃；1000 RPS 完整施压的 60 秒轮次中评论列表 p95 534.27ms 未达标，后续诊断见上节。历史轮次保留，不作为本次业务优化收益。

此前业务混合验证见 [业务流程混合负载验证](#业务流程混合负载验证2026-10-11)：预期 1000 HTTP RPS、30 秒升速、10 分钟稳定窗口；实际成功 866.75 RPS，10 个接口 p95 均未达标，丢弃 17439 个流程，评论列表发生一次超时。Outbox 峰值 130 条 / 2.763 秒、无重试，后台为“需确认”。不能用此前单接口通过档位推断整站混合容量。

最新时间列表结果见 [时间列表查询优化压测](#时间列表查询优化压测2026-10-11)：新版四档短测通过，2500 RPS 同参数单轮 p95 从历史 1660.51ms 降至 126.84ms，观测下降 92.36%；但 2500 RPS 三轮各 180 秒仅 2/3 轮通过，第一轮丢弃 1974 次。旧版直接复用历史数据，没有重测；不声明该档持续稳定通过，也不计算正式三轮 A/B 或吞吐提升率。此前“优先验证列表查询收敛”的建议属于优化前，查询收敛已由 `20f0b14` 实施，本次只测量。

最新详情结果见 [详情查询优化压测](#详情查询优化压测2026-10-11)：新版短测通过 2500 RPS；2000 RPS 三轮各 180 秒均通过，p95 中位数 4.75ms，零错误、零丢弃。按用户要求直接复用旧版历史数据，旧版与新版的收益对照仅使用同参数 60 秒单轮，不是正式三轮 A/B。[此前全量复跑](#全量压测复跑2026-10-11)中的详情失败属于优化前；发帖、投票通过 300 RPS、500 RPS 未通过。下一步优先级见[优化方向](#详情优化后的下一步方向2026-10-11)。以下早期结果和优化建议保留为历史记录。

初次探测日期：2026-10-10；Outbox 批量优化 A/B 日期：2026-10-11，结果见本文末尾。以下先保留六核心场景脚本的初次探测结果，不是历史 2026-10-09 基线。每档升速 10 秒、稳定施压 60 秒，每次迭代只有一个目标请求。通过条件：p95 < 500ms、HTTP 和业务失败率均 < 1%、零丢弃；写接口还需后台未见持续积压。

HTML 探测总览：[打开图表](./output/results/probe-20261010-163945.html)。全部新版轮次见 [HTML 索引](./output/results/index.html)。原始结果在本机被忽略的 output 目录，保留本页数值用于追溯；完整使用方式见 [README](./README.md)。

## 初次探测：六接口的已测区间

| 接口 | 最高已测通过 RPS | 通过档位 p95 ms | 首次未通过 RPS | 未通过的证据 |
| --- | ---: | ---: | ---: | --- |
| 注册 | 100 | 63.46 | 150 | p95 1836.35ms，丢弃 1114 次 |
| 登录 | 100 | 48.17 | 150 | p95 2126.12ms，丢弃 839 次 |
| 帖子详情 | 2000 | 177.98 | 2500 | p95 1685.00ms，丢弃 21197 次 |
| 时间列表 | 2500 | 159.81 | 未找到 | 本轮最高测试 2500；不推断精确上限 |
| 发帖 | 100 | 24.22 | 200 | HTTP p95 28.00ms，但后台积压 |
| 投票 | 100 | 23.39 | 200 | HTTP p95 29.57ms，丢弃 5 次且后台积压 |

通过数字是短测下界，不是生产容量，接口 RPS 不能相加。有丢弃的轮次不是精确后端吞吐上限。

## 初次探测：优先处理写链路

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

## 全量压测复跑（2026-10-11）

**六接口冒烟全部通过；19 个逐档探测中 13 档通过、6 档未通过。下一项优先验证帖子详情的查询收敛。** 发帖和投票均通过 300 RPS；500 RPS 时 HTTP 延迟、丢弃与后台积压同时出现，仍需后续诊断。详情在 2000 RPS 失败，而同轮时间列表 2000 RPS 通过，提供了更明确、改动范围较小的读链路优化候选。

[六接口探测总览](./output/results/probe-20261011-212640.html)、[冒烟总览](./output/results/smoke-20261011-210029.html)、[全量索引](./output/results/index.html)、[逐轮台账](./records.csv)。全部 25 轮摘要、原始目录、p99、配置和指纹另存 [分析 JSON](./output/results/full-probe-20261011-2058/analysis.json)。测试时间为北京时间 20:58:25–21:26:43，约 28 分钟。

### 条件与命令

- 提交 `f59f0518905f7c44faf298d604f95cfba855a5c5`；沿用本机 WSL2、i9-13900HX、6 逻辑 CPU、4012868 KiB 内存，Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。后端、数据库和 k6 共享硬件。
- 每轮恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票；MySQL `bluebell_k6_verify`、Redis DB13、端口 18080，连接池最大/空闲均为 10，info/JSON 请求日志，发帖正文 4 KiB。
- 冒烟为 1 RPS、10 秒、无升速；探测为 10 秒升速 + 60 秒稳定施压，每档一次，VU 使用脚本默认值。首次未通过后停止该接口加压，再测试下一接口。判定阈值与两秒后台采样保持不变。
- 测量脚本指纹 `39e5229853ae8ac248a29a047ef8d45e4f7dd5a42f5d55b8a99c1a583754b56d`，快照指纹 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`。
- 冒烟二进制指纹 `373444bde1f22941c1e8b1339d72c143235351b9ab992efe0bda028619226926`；19 轮探测均为 `bbbe969d44fe8c4ac55bd1fe7dc5f0d3a10a7509de7a13988997dc51094ea91e`。两阶段分别编译，冒烟后自动更新台账使工作区变为 dirty；探测程序的 [构建信息](./output/results/full-probe-20261011-2058/server-build-info.txt) 包含 `vcs.modified=true`。提交相同，本轮没有修改业务源码；跨阶段指纹分别记录，不声称 25 轮使用同一二进制。

准确编排见 [执行脚本](./output/results/full-probe-20261011-2058-run.sh)和[完整驱动日志](./output/results/full-probe-20261011-2058/driver.log)，核心命令如下。驱动先清除外部负载参数，冒烟后探测，退出时恢复数据。

```bash
GOCACHE=/tmp/bluebell-go-cache tests/load/run.sh smoke
GOCACHE=/tmp/bluebell-go-cache tests/load/run.sh probe
GOCACHE=/tmp/bluebell-go-cache tests/load/run.sh reset
```

### 六接口边界

通过档位的失败率和丢弃均为 0；写接口同时满足后台标准。

| 接口 | 本轮最高已测通过 RPS | 通过档位 p95 ms | 首次未通过 RPS | 未通过的证据 |
| --- | ---: | ---: | ---: | --- |
| 注册 | 100 | 68.46 | 150 | p95 1937.75ms，丢弃 1335 |
| 登录 | 100 | 51.26 | 150 | p95 2168.86ms，丢弃 993，失败 0.075% |
| 帖子详情 | 1500 | 10.76 | 2000 | p95 1433.42ms，丢弃 3384 |
| 时间列表 | 2000 | 34.31 | 2500 | p95 1660.51ms，丢弃 9735 |
| 发帖 | 300 | 36.27 | 500 | p95 2200.95ms，丢弃 5889，后台持续积压 |
| 投票 | 300 | 30.17 | 500 | p95 2678.98ms，丢弃 5178，后台持续积压 |

### 全部探测轮次

本轮各档 HTTP 与业务失败率相同，表中合并展示；后台峰值仅取稳定阶段，单位为条 / 秒。点击目标档位打开原始单轮报告，p99 等完整指标见分析 JSON 与 CSV。

| 接口 | 目标 RPS / 报告 | 成功 RPS | p95 ms | HTTP / 业务失败率 | 丢弃 | 后台判定 | 后台峰值 条 / 秒 | 结果 |
| --- | ---: | ---: | ---: | ---: | ---: | --- | --- | --- |
| 注册 | [100](./output/results/20261011-210034124398396-current-signup-100/report.html) | 100.00 | 68.46 | 0.000% | 0 | 不适用 | — | 通过 |
| 注册 | [150](./output/results/20261011-210148386511482-current-signup-150/report.html) | 127.75 | 1937.75 | 0.000% | 1335 | 不适用 | — | 未通过 |
| 登录 | [100](./output/results/20261011-210301792886658-current-login-100/report.html) | 100.00 | 51.26 | 0.000% | 0 | 不适用 | — | 通过 |
| 登录 | [150](./output/results/20261011-210414874465732-current-login-150/report.html) | 133.35 | 2168.86 | 0.075% | 993 | 不适用 | — | 未通过 |
| 详情 | [1000](./output/results/20261011-210528748238700-current-post-1000/report.html) | 1000.00 | 5.31 | 0.000% | 0 | 不适用 | — | 通过 |
| 详情 | [1500](./output/results/20261011-210642002974439-current-post-1500/report.html) | 1500.00 | 10.76 | 0.000% | 0 | 不适用 | — | 通过 |
| 详情 | [2000](./output/results/20261011-210756604934723-current-post-2000/report.html) | 1943.63 | 1433.42 | 0.000% | 3384 | 不适用 | — | 未通过 |
| 时间列表 | [1000](./output/results/20261011-210910953005024-current-posts_time-1000/report.html) | 1000.02 | 4.59 | 0.000% | 0 | 不适用 | — | 通过 |
| 时间列表 | [1500](./output/results/20261011-211024259742488-current-posts_time-1500/report.html) | 1499.95 | 6.33 | 0.000% | 0 | 不适用 | — | 通过 |
| 时间列表 | [2000](./output/results/20261011-211137647109578-current-posts_time-2000/report.html) | 2000.00 | 34.31 | 0.000% | 0 | 不适用 | — | 通过 |
| 时间列表 | [2500](./output/results/20261011-211251142310510-current-posts_time-2500/report.html) | 2337.73 | 1660.51 | 0.000% | 9735 | 不适用 | — | 未通过 |
| 发帖 | [100](./output/results/20261011-211405990905947-current-create_post-100/report.html) | 100.00 | 24.82 | 0.000% | 0 | 未见持续积压 | 38 / 0.384 | 通过 |
| 发帖 | [200](./output/results/20261011-211538800081234-current-create_post-200/report.html) | 200.00 | 29.31 | 0.000% | 0 | 未见持续积压 | 107 / 0.528 | 通过 |
| 发帖 | [300](./output/results/20261011-211713735771064-current-create_post-300/report.html) | 300.00 | 36.27 | 0.000% | 0 | 未见持续积压 | 16 / 0.238 | 通过 |
| 发帖 | [500](./output/results/20261011-211848644865985-current-create_post-500/report.html) | 401.77 | 2200.95 | 0.021% | 5889 | 持续积压 | 12425 / 31.330 | 未通过 |
| 投票 | [100](./output/results/20261011-212023719451213-current-vote-100/report.html) | 100.00 | 24.39 | 0.000% | 0 | 未见持续积压 | 5 / 0.054 | 通过 |
| 投票 | [200](./output/results/20261011-212156638405747-current-vote-200/report.html) | 200.00 | 27.47 | 0.000% | 0 | 未见持续积压 | 10 / 0.068 | 通过 |
| 投票 | [300](./output/results/20261011-212331505122958-current-vote-300/report.html) | 300.02 | 30.17 | 0.000% | 0 | 未见持续积压 | 16 / 0.067 | 通过 |
| 投票 | [500](./output/results/20261011-212506901183919-current-vote-500/report.html) | 412.20 | 2678.98 | 0.363% | 5178 | 持续积压 | 19497 / 47.893 | 未通过 |

### 下一项优化：详情查询收敛

优先级依据是已有性能问题、明确的代码切入点和较小的改动范围，而不是接口 RPS 数字的高低：

1. **同轮读接口对比**：2000 RPS 下详情 p95 1433.42ms、有丢弃，时间列表 p95 34.31ms、零丢弃。这说明详情是较早退化的读链路，但两个请求返回数据和查询方式不同，不能将差值全部归因于查询次数。
2. **源码证据**：`internal/service/post.go` 的 `Detail` 依次查询帖子、作者、社区、投票统计，每请求四次 MySQL 查询。2000 RPS 目标意味着约 8000 次查询/秒的理论请求需求，不是本轮实测 SQL 吞吐。候选方案是将帖子、作者、社区合并读取，票数继续从 MySQL 实时聚合，先验证四次查询收敛到两次；是否继续合成一次以实际计划与耗时为准。
3. **日志证据与边界**：[详情失败轮日志](./output/results/20261011-210756604934723-current-post-2000/server.log)中作者、社区和票数均有数百毫秒记录；[发帖 500 RPS 日志](./output/results/20261011-211848644865985-current-create_post-500/server.log)中社区主键查询也出现秒级耗时。GORM 耗时包含应用侧等待，日志仅保留最后 200 行，不能据此断言某个索引缺失、连接池饱和或给出完整 SQL 分布。

下一步先在详情 2000 RPS 固定参数下记录三轮 180 秒基线，同时区分连接池等待、SQL 执行与 HTTP 耗时；再单独验证查询收敛，保持连接池、数据与日志设置不变，三轮同条件复测。验收需包含帖子/关联对象不存在、软删除、零票、正反票及取消票的语义；收益以正式对照为准，不能承诺未测的提升比例。实施涉及 service/repository，应先确认具体 Implementation Plan。

另外两项保留为后续方向：

- **500 RPS 写链路**：HTTP 和后台同时退化，投票还存在取消上下文错误；需要拆分 API 事务、Outbox 查询/投影/确认、连接等待和 COMMIT 耗时。不能据此直接增加 Worker 并发或认定确认删除再次成为根因。300 RPS 的两条写链路均通过，因此本轮将这项排在范围更明确的详情之后；这不是断言写链路问题较轻。
- **认证 150 RPS**：两接口均延迟超标并出现丢弃；源码使用 bcrypt DefaultCost，CPU 成本是待核验候选，本轮没有 CPU profile，尚未证实根因。维持密码处理语义，后续用 profile 与资源隔离验证，不将降低 bcrypt 成本当成默认优化。

### 核验与收尾

| 项目 | 结果与证据 |
| --- | --- |
| 测量完整性 | 驱动退出码 0，25 轮完整归档，无运行异常；6 个未通过档位均保留；冒烟不判断后台容量 |
| 离线数据核验 | `python3 tests/load/output/results/full-probe-20261011-2058/verify.py` 退出码 0，逐项核对摘要、CSV、HTTP 判定、退出码、后台稳定窗口及判定、指纹；[脚本](./output/results/full-probe-20261011-2058/verify.py)、[通过日志](./output/results/full-probe-20261011-2058/verify.log) |
| 核验器修正记录 | 前两次核验退出码 1：首次将报告 `verdict=1` 误当作进程码 99；第二次误要求冒烟/探测两次构建的指纹相同。按报告源码及实际阶段修正断言后通过，未修改压测结果；[首次日志](./output/results/full-probe-20261011-2058/verify-first.log)、[第二次日志](./output/results/full-probe-20261011-2058/verify-second.log) |
| 台账 | 新增 25 轮，新版总计 82 轮；历史 57 轮保留，未删除失败轮次 |
| 数据恢复 | reset 退出码 0；SQL 数量 200 / 5 / 2000 / 20000 / 20000，Outbox 0；Redis 时间与热度全局投影各 2000，18080 无监听；[reset 日志](./output/results/full-probe-20261011-2058/reset.log)、[只读核验](./output/results/full-probe-20261011-2058/restored-state.log) |
| 改动范围 | 仅更新发现文档和自动生成台账；业务代码、服务配置及测量脚本没有修改，本轮按用户明确后的要求只做全量压测，未运行 Go 全量回归 |

本轮是每档一次的 60 秒容量探测，不是优化 A/B 或生产容量测试。详情 2000 RPS、列表 2500 RPS 的结果比初次探测差，存在环境负载、缓存和调度波动的可能；本轮没有隔离这些因素，不能宣称业务代码发生性能回归，也不与历史样本直接计算收益。丢弃请求未被实际发出，未通过档位不是精确吞吐上限；后台两秒采样可能漏峰值，最老等待峰值也不是逐条同步时延分位数。原始 HTML、JSON、日志仍在被 Git 忽略的 output 目录，分享时需一同保留。


## 详情查询优化压测（2026-10-11）

**新版四档短测均通过，最高已测 2500 RPS；2000 RPS 三轮各 180 秒全部通过，p95 中位数 4.75ms，零错误、零丢弃。** 旧版按用户要求直接复用此前全量复跑的历史数据；同参数 2000 RPS、60 秒单轮的 p95 从 1433.42ms 降至 4.93ms，观测降幅 99.66%。这个百分比只描述两次单轮测量，不能作为正式三轮 A/B 收益。

[历史对比 HTML](./output/results/post-detail-20261011/report.html)、[新版逐档探测](./output/results/probe-20261011-220711.html)、[新版三轮报告](./output/results/after-20261011-222124.html)、[分析 JSON](./output/results/post-detail-20261011/analysis.json)、[逐轮台账](./records.csv)。本次新版测量时间为北京时间 22:01–22:21。

### 改动与测试条件

用户已提交的优化版本为 `b607714e8d122ecd8a329bf542b0c1cbda416f62`：`PostService.Detail` 将帖子、作者、社区三个独立查询替换为 `FindDetailByID` 的 LEFT JOIN，票数仍从 MySQL 实时聚合，每请求 SQL 从四次收敛为两次。本轮只执行测量、生成报告和更新文档，没有修改业务代码、配置或测量脚本。

旧数据取自 `f59f051` 的三档详情探测，该版本与优化直接父提交 `6074a4c` 的业务源码、go.mod、go.sum 一致。新旧记录的环境、配置、数据数量、测量脚本与快照指纹逐项核验一致：

- WSL2 / i9-13900HX，6 逻辑 CPU，4012868 KiB 内存；Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。后端、k6 和数据库共享本机硬件。
- MySQL 最大 / 空闲连接数均为 10，info / JSON 请求日志；独立库 `bluebell_k6_verify`、Redis DB13、端口 18080。每轮恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票，随机读取帖子。
- 脚本指纹 `39e5229853ae8ac248a29a047ef8d45e4f7dd5a42f5d55b8a99c1a583754b56d`，快照指纹 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`。
- 新版四档探测及三轮稳定性测量的程序指纹均为 `9c12a980511d65f4af7d7668c59e2cc3654c766fa99f2e6b4d13b2d43fc1324a`。HTTP 阈值保持 p95 < 500ms、HTTP / 业务失败率均 < 1%、零丢弃。

执行命令如下，完整日志见[冒烟](./output/results/post-detail-smoke.log)、[探测](./output/results/post-detail-probe.log)、[新版三轮](./output/results/post-detail-20261011/after.log)、[恢复](./output/results/post-detail-20261011/reset.log)。

```bash
GOCACHE=/tmp/bluebell-go-cache make loadtest ENDPOINT=post RATE=1 VUS=10 RAMP_SECONDS=0 HOLD_SECONDS=10
GOCACHE=/tmp/bluebell-go-cache make loadtest-probe ENDPOINT=post
GOCACHE=/tmp/bluebell-go-cache make loadtest-repeat ENDPOINT=post RATE=2000 VUS=1505 RAMP_SECONDS=10 LABEL=after
GOCACHE=/tmp/bluebell-go-cache make loadtest-reset
```

### 复用历史数据：同档位 60 秒单轮

每档升速 10 秒、稳定 60 秒；1000 / 1500 / 2000 RPS 的 VU 分别为 755 / 1130 / 1505。新旧 HTTP 和业务失败率均为 0。每个版本每档只有一次测量，表中降幅不包含重复测量的不确定性。

| 目标 RPS | 旧版报告 | 旧 p95 / p99 ms | 旧成功 RPS / 丢弃 | 新版报告 | 新 p95 / p99 ms | 新成功 RPS / 丢弃 | 单轮 p95 降幅 | 判定 |
| --- | --- | ---: | ---: | --- | ---: | ---: | ---: | --- |
| 1000 | [查看](./output/results/20261011-210528748238700-current-post-1000/report.html) | 5.31 / 12.22 | 1000.00 / 0 | [查看](./output/results/20261011-220217054664651-current-post-1000/report.html) | 3.06 / 5.25 | 1000.00 / 0 | 42.45% | 通过 → 通过 |
| 1500 | [查看](./output/results/20261011-210642002974439-current-post-1500/report.html) | 10.76 / 40.36 | 1500.00 / 0 | [查看](./output/results/20261011-220331600586978-current-post-1500/report.html) | 3.42 / 10.59 | 1500.02 / 0 | 68.19% | 通过 → 通过 |
| 2000 | [查看](./output/results/20261011-210756604934723-current-post-2000/report.html) | 1433.42 / 1870.51 | 1943.63 / 3384 | [查看](./output/results/20261011-220445374006113-current-post-2000/report.html) | 4.93 / 23.39 | 2000.02 / 0 | 99.66% | 未通过 → 通过 |

新版额外测试 [2500 RPS](./output/results/20261011-220559144843805-current-post-2500/report.html)：1880 VU，成功 2500.02 RPS，p95 17.72ms、p99 61.44ms，错误和丢弃均为 0。旧版最新探测在 2000 RPS 首次失败后停止，没有同组 2500 RPS 历史样本。

短测最高已测通过档位由历史 1500 升至新版 2500 RPS；2500 未做三轮 180 秒验证，没有继续寻找失败档位，因此不计算正式吞吐提升率，不宣称最大容量。2026-10-09 和首次探测的数据不混入本次收益计算。

### 新版 2000 RPS：三轮 180 秒稳定性

三轮均为 1505 VU、升速 10 秒、稳定施压 180 秒，每轮从同一快照恢复；错误和丢弃均为 0。

| 轮次 / 报告 | 成功请求数 | 成功 RPS | p95 ms | p99 ms | 判定 |
| --- | ---: | ---: | ---: | ---: | --- |
| [1](./output/results/20261011-221143628599755-after-post-2000/report.html) | 360003 | 2000.02 | 4.58 | 20.49 | 通过 |
| [2](./output/results/20261011-221458394584674-after-post-2000/report.html) | 360001 | 2000.01 | 4.75 | 21.98 | 通过 |
| [3](./output/results/20261011-221812308417213-after-post-2000/report.html) | 360003 | 2000.02 | 4.87 | 24.00 | 通过 |

三轮共成功 **1080007 次**，p95 中位数 **4.75ms**、p99 中位数 **21.98ms**，成功 RPS 中位数 **2000.02**。这组测量验证新版在当前条件下承载 2000 RPS；旧版历史只有单轮 60 秒，不能将上述三轮中位数与旧版单轮直接计算正式 A/B 百分比。

### 已停止的旧版复测

用户补充“无需重测旧版，直接复用旧版数据”后，停止旧版复测并切换为仅测试新版。停止前，`SERVER_SOURCE_DIR=/tmp/bluebell-post-before-6074a4c`、`LABEL=before`、2000 RPS / 1505 VU 的[第一轮](./output/results/20261011-220724437757974-before-post-2000/report.html)已完成；[第二轮](./output/results/20261011-221041041899293-before-post-2000/report.html)被终止，退出码 143、台账标记“运行异常”。两轮均保留原始证据，**不参加最终性能比较**。停止日志见[旧版复测日志](./output/results/post-detail-20261011/before.log)，中止原因见[记录](./output/results/20261011-221041041899293-before-post-2000/aborted.json)。

### 核验与收尾

| 项目 | 结果与证据 |
| --- | --- |
| 新版测量 | 1 轮冒烟、4 档短测、3 轮稳定性检查全部通过；三轮组退出码 0 |
| 数据核验 | `python3 tests/load/output/results/post-detail-20261011/verify.py` 退出码 0；逐项核对摘要、原始文件、台账、判定、环境、配置和指纹，并核对中止轮为运行异常；[脚本](./output/results/post-detail-20261011/verify.py)、[日志](./output/results/post-detail-20261011/verify.log) |
| 详情计票回归 | `GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run '^TestPost_DetailCountsVotesByPostID$' -count=1 -v` 退出码 0，0.463 秒；现有正反票、取消票及分帖计数测试通过；[日志](./output/results/post-detail-20261011/detail-test.log)。未运行全量回归或竞态检查 |
| 对比报告 | 使用原生 k6 离线读取分析 JSON 生成历史对比 HTML；[报告脚本](./output/results/post-detail-20261011/report.js)、[生成日志](./output/results/post-detail-20261011/report.log)；没有调用需要旧版三轮的 `loadtest-compare`，没有伪造或重复历史样本 |
| 台账 | 新增 10 轮：新版 8 轮、排除的旧版完成轮 1 轮、中止轮 1 轮；新版台账合计 92 轮，历史记录保留 |
| 数据恢复 | reset 退出码 0；SQL 数量 200 / 5 / 2000 / 20000 / 20000，Outbox 0；Redis DB13 时间与热度全局索引各 2000，18080 无监听；[只读核验](./output/results/post-detail-20261011/restored-state.log) |

记录的环境字段一致不等于实时资源负载完全相同，历史与当前单轮差异仍包含缓存和调度波动。旧版 2000 RPS 有丢弃，成功 RPS 不是精确后端吞吐上限；本次没有采集连接池等待或 SQL profile，不能据 HTTP 数字进一步断言数据库等待的具体占比。只验证了现有详情计票用例，未额外覆盖关联对象缺失或软删除边界。原始 HTML、JSON、日志位于被 Git 忽略的 output 目录，分享时须保留本次报告及引用的历史、新版轮次目录。

## 详情优化后的下一步方向（2026-10-11）

**若继续做性能专项，优先验证时间列表的查询收敛，其次诊断 500 RPS 写链路，认证接口排第三。** 这是根据当前代码和已有测量提出的候选顺序，不代表一般业务必须逐项实施；优化必要性取决于实际峰值、用户延迟目标和后台同步要求。尚未实施或测量候选优化的收益。

| 优先级 | 方向 | 已有证据 | 下一步验证 |
| --- | --- | --- | --- |
| 1 | 时间列表：三次 MySQL 批量查询收敛为一次连表查询 | 历史 2000 RPS 通过，p95 34.31ms；2500 RPS p95 1660.51ms、丢弃 9735。`PostService.List` 依次读取帖子、作者名、社区名 | 固定 2500 RPS、1880 VU，验证连表后延迟、丢弃与结果语义，再做新版三轮 180 秒稳定性检查 |
| 2 | 发帖 / 投票：拆分 API 事务与 Outbox 的耗时 | 两接口 300 RPS 通过；500 RPS p95 分别 2200.95 / 2678.98ms，同时丢弃、持续积压 | 在 300 / 500 RPS 分别采集连接池等待增量、事务 / COMMIT、Worker 查询、Redis、确认删除的耗时；按证据选一个变量优化 |
| 3 | 注册 / 登录：确认 bcrypt 的 CPU 占比 | 100 RPS 通过；150 RPS 延迟约 1938 / 2169ms，均有丢弃；代码使用 bcrypt DefaultCost | 采集 CPU profile 和服务 / k6 资源使用，再决定请求并发控制或 CPU 容量规划；保持密码哈希成本 |

时间列表候选只改变 MySQL 取数：用帖子 ID 集合连表读取列表字段，继续按 Redis 返回的 ID 顺序排列，并保留帖子软删除过滤、空页、社区范围、关联对象缺失时的现有处理。当前 `FindByIDs` 已显式选择列表列，`FindPostIDs` 已用 Redis Pipeline 读取 ID 和总数；这两点已经落实，无需再作为新增优化。可实现性参考 [GORM Joins / Select 官方文档](https://gorm.io/docs/query.html#Joins)，是否改善吞吐仍需本项目实测。

列表验收覆盖时间 / 热度排序、社区过滤、空页、软删除后 Redis 残留 ID、作者或社区缺失，以及响应字段。先保持连接池 10、数据快照和日志设置不变，只验证查询收敛。按用户要求复用旧数据：与历史同参数 60 秒单轮比较，三轮 180 秒只验证新版稳定性；没有旧版三轮时，收益仍按单轮观测标注。三轮若通过 2500 RPS，再探测 3000 RPS，不能预先承诺该档通过。

写链路的根因尚未定位。当前 Outbox 已按批次查询、Pipeline 同步并强制使用主键删除，旧的确认删除锁范围诊断不能直接作为当前 500 RPS 失败的归因。连接等待可通过 `sql.DB.Stats()` 的 `WaitCount` / `WaitDuration` 区间增量观察；它们是共享连接池的累计指标，不能直接当成某个接口或单条 SQL 的耗时。参考 [Go 官方连接池文档](https://go.dev/doc/database/manage-connections)。增加连接数、Worker 并发或拆分连接池都应在测量后作为独立候选验证。

详情目前已有 2000 RPS 三轮通过的证据，暂不继续增加票数缓存、异步计数或新索引。后续先补齐关联对象缺失、软删除等正确性边界；出现更大数据规模或热点帖子下的具体性能问题，再测实时票数聚合的查询计划。

本节只记录建议并更新文档，没有执行新增压测、插桩或业务变更。列表修改涉及 service / repository，编码前仍需输出 Implementation Plan 并确认。

### 一般业务下是否需要继续优化

**现有证据不要求立即继续优化发帖 / 投票或注册 / 登录。** 如果实际峰值明显低于已测通过档位，延迟、错误率和列表 / 排序可见延迟满足业务目标，可以保持当前实现。500 RPS 写入、150 RPS 认证的失败描述已测边界，不能单独证明一般业务容量不足。优化应由用户需要的服务指标驱动，参考 [Google SRE 服务目标](https://sre.google/sre-book/service-level-objectives/)。

| 接口 | 当前通过档位的证据 | 建议重新启动优化的条件 |
| --- | --- | --- |
| 发帖 / 投票 | 各自单接口 300 RPS、60 秒通过，p95 36.27 / 30.17ms，零错误、零丢弃，后台未见持续积压 | 预计峰值接近已测边界、混合流量下 HTTP 超标，或 Outbox 等待持续增加 / 投影可见延迟超过业务目标 |
| 注册 / 登录 | 各自单接口 100 RPS、60 秒通过，p95 68.46 / 51.26ms，零错误、零丢弃 | 集中注册 / 登录峰值导致排队，认证占用 CPU 影响其他接口，或响应延迟 / 错误率超出业务目标 |

这些数字来自小规模固定数据集、同机单接口短测，不能当作生产容量，也不能相加成整站吞吐。当时更有价值的验证是按预期请求比例做混合负载并延长稳定窗口，检查 HTTP 和 Outbox 的同步表现；若业务会集中投票到少数热门帖子，应加入热点分布。后续已执行，见 [业务流程混合负载验证](#业务流程混合负载验证2026-10-11)。

本轮据用户补充的业务视角调整建议并维护文档，没有新增性能或功能测试。

## 时间列表查询优化压测（2026-10-11）

**新版四档 60 秒短测均通过；2500 RPS 单轮 p95 从旧版 1660.51ms 降至 126.84ms，观测下降 92.36%，丢弃从 9735 降至 0。但新版 2500 RPS 三轮各 180 秒仅 2/3 轮通过，不能声明该档持续稳定通过。** 旧版按用户要求直接复用已有数据，没有重测。历史只有单轮 60 秒，不计算正式三轮 A/B 收益或正式吞吐提升率。

[历史对比 HTML](./output/results/posts-time-20261011/report.html)、[新版逐档探测](./output/results/probe-20261011-225724.html)、[新版三轮报告](./output/results/after-20261011-230736.html)、[分析 JSON](./output/results/posts-time-20261011/analysis.json)、[逐轮台账](./records.csv)。本次新版测量时间为北京时间 22:51–23:07，回归与恢复在 23:08 完成。

### 改动与对照条件

用户提交的优化版本为 `20f0b148107f481d735a7b37518651a804af296f`：`PostService.List` 将帖子、作者、社区三次 MySQL 批量查询替换为 `FindListByIDs` 的一次 JOIN，继续按 Redis 返回的 ID 顺序构建响应。本次只运行压测、核验并更新报告与文档，没有修改业务代码、配置或测量脚本。

历史取自最近一次全量复跑 `f59f051` 的四档时间列表结果，不混用 2026-10-09 或 2026-10-10 的数据。历史版本与优化直接父提交 `c8eca75` 的 `List`、`FindByIDs`、作者与社区名称批量查询函数逐项一致，Redis 列表实现、go.sum 与依赖版本一致；go.mod 仅将同版本 MySQL 驱动从 indirect 调整为 direct。核验见[源码对照日志](./output/results/posts-time-20261011/source-check.log)。中间的详情查询优化不改变列表调用路径。

新旧四档的负载、VU、时长、配置、数据及记录的环境完全一致：

- 每档升速 10 秒、稳定 60 秒；1000 / 1500 / 2000 / 2500 RPS 分别使用 755 / 1130 / 1505 / 1880 VU；遍历时间列表前 20 页，每页 10 条。
- WSL2 / i9-13900HX，6 逻辑 CPU，4012868 KiB 内存；Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。后端、k6 与数据库共享本机硬件。
- MySQL 最大 / 空闲连接数均为 10，info / JSON 日志；独立库 `bluebell_k6_verify`、Redis DB13、端口 18080。每轮恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票。
- 脚本指纹 `39e5229853ae8ac248a29a047ef8d45e4f7dd5a42f5d55b8a99c1a583754b56d`；快照指纹 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`。
- 新版四档探测和三轮测量的程序指纹均为 `919dbc6f821eacd59e97832db1675028034497d383ef468ae670153c2890d783`。通过标准保持 p95 < 500ms、HTTP / 业务失败率均 < 1%、零丢弃。

执行命令如下，完整日志见[冒烟](./output/results/posts-time-20261011/smoke.log)、[探测](./output/results/posts-time-20261011/probe.log)、[三轮](./output/results/posts-time-20261011/after.log)、[回归](./output/results/posts-time-20261011/list-test.log)、[恢复](./output/results/posts-time-20261011/reset.log)。

```bash
GOCACHE=/tmp/bluebell-go-cache make loadtest ENDPOINT=posts_time RATE=1 VUS=10 RAMP_SECONDS=0 HOLD_SECONDS=10
GOCACHE=/tmp/bluebell-go-cache make loadtest-probe ENDPOINT=posts_time
GOCACHE=/tmp/bluebell-go-cache make loadtest-repeat ENDPOINT=posts_time RATE=2500 VUS=1880 RAMP_SECONDS=10 LABEL=after
GOCACHE=/tmp/bluebell-go-cache go test ./internal/service -run '^TestPost_(ListRejectsInvalidPagination|CreateAndDeleteSyncRanking)$' -count=1 -v
GOCACHE=/tmp/bluebell-go-cache make loadtest-reset
BLUEBELL_CONFIG_FILE="$PWD/tests/load/config.yaml" tests/load/output/bin/server
# 服务就绪后，另一个终端执行；核验完关闭该服务
python3 tests/load/output/results/posts-time-20261011/check-list.py
```

### 历史与新版：同参数 60 秒单轮

新旧四档 HTTP / 业务失败率均为 0。每个版本每档仅一次测量；降幅只描述单轮观测，不包含重复测量的不确定性。

| 目标 RPS | 旧版报告 | 旧 p95 / p99 ms | 旧成功 RPS / 丢弃 | 新版报告 | 新 p95 / p99 ms | 新成功 RPS / 丢弃 | 单轮 p95 降幅 | 判定 |
| --- | --- | ---: | ---: | --- | ---: | ---: | ---: | --- |
| 1000 | [查看](./output/results/20261011-210910953005024-current-posts_time-1000/report.html) | 4.59 / 8.39 | 1000.02 / 0 | [查看](./output/results/20261011-225228289074950-current-posts_time-1000/report.html) | 2.40 / 4.45 | 1000.03 / 0 | 47.69% | 通过 → 通过 |
| 1500 | [查看](./output/results/20261011-211024259742488-current-posts_time-1500/report.html) | 6.33 / 27.62 | 1499.95 / 0 | [查看](./output/results/20261011-225343130018945-current-posts_time-1500/report.html) | 3.53 / 9.53 | 1500.03 / 0 | 44.22% | 通过 → 通过 |
| 2000 | [查看](./output/results/20261011-211137647109578-current-posts_time-2000/report.html) | 34.31 / 79.83 | 2000.00 / 0 | [查看](./output/results/20261011-225456965322573-current-posts_time-2000/report.html) | 8.21 / 43.46 | 2000.15 / 0 | 76.07% | 通过 → 通过 |
| 2500 | [查看](./output/results/20261011-211251142310510-current-posts_time-2500/report.html) | 1660.51 / 2223.86 | 2337.73 / 9735 | [查看](./output/results/20261011-225612171177359-current-posts_time-2500/report.html) | 126.84 / 342.43 | 2500.03 / 0 | 92.36% | 未通过 → 通过 |

短测最高已测通过档位由历史 2000 升至新版 2500 RPS；不据此计算正式吞吐提升率。固定目标负载下的成功 RPS 差异不能写成最大吞吐提升，旧版有丢弃的 2500 RPS 也不是精确后端上限。

### 新版 2500 RPS：三轮 180 秒

三轮均为 1880 VU、升速 10 秒、稳定 180 秒；每轮恢复同一快照。保留全部轮次，不挑选后两轮计算稳定通过结论。

| 轮次 / 报告 | 成功请求数 | 成功 RPS | p95 / p99 ms | HTTP / 业务失败率 | 丢弃 | 判定 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| [1](./output/results/20261011-225753219733660-after-posts_time-2500/report.html) | 447972 | 2488.73 | 409.55 / 1554.90 | 0.0105% / 0.0105% | 1974 | 未通过 |
| [2](./output/results/20261011-230108769457094-after-posts_time-2500/report.html) | 450007 | 2500.04 | 111.20 / 498.06 | 0 / 0 | 0 | 通过 |
| [3](./output/results/20261011-230423260071584-after-posts_time-2500/report.html) | 449998 | 2499.99 | 28.71 / 189.81 | 0 / 0 | 0 | 通过 |

三轮共成功 **1347977 次**，p95 中位数 **111.20ms**、p99 中位数 **498.06ms**、成功 RPS 中位数 **2499.99**。第一轮稳定阶段有 47 次请求失败，p95 与失败率虽满足阈值，但丢弃不为零，整组三轮未全部通过。延迟范围 28.71–409.55ms，存在明显轮间波动；中位数不能掩盖失败轮。

没有追加 3000 RPS：此前计划要求 2500 RPS 三轮全部通过才继续加压。没有另测 2000 RPS 的三轮，因此本次没有新的“三轮全部通过”容量档位。若继续验证，应先解释 2500 RPS 的超时、丢弃与波动，或在 2000 RPS 做三轮稳定性检查；当前数据不能区分服务、k6、数据库或同机调度的具体影响。

### 核验、收尾与限制

| 项目 | 结果与证据 |
| --- | --- |
| 新版测量 | 1 轮冒烟、4 档短测通过；2500 RPS 三轮仅 2/3 通过；三个原始退出码为 99 / 0 / 0 |
| 阈值退出与收尾 | `run.sh repeat` 的预期退出码 99 被 make 转为 2；三轮已完整生成，最初编排在此退出，随后使用 [finish.sh](./output/results/posts-time-20261011/finish.sh) 完成回归与恢复，退出码 0。没有重跑施压；原始 make 退出码保留在 [after-exit.txt](./output/results/posts-time-20261011/after-exit.txt) |
| 指标与条件 | `python3 tests/load/output/results/posts-time-20261011/verify.py` 退出码 0；新版 8 轮与历史 4 轮的摘要、台账、原始文件、阈值判定一致；同参数四档环境、配置、数据与指纹一致；[脚本](./output/results/posts-time-20261011/verify.py)、[日志](./output/results/posts-time-20261011/verify.log) |
| 列表回归 | 上述两个现有服务测试通过，0.249 秒，退出码 0；覆盖非法分页、社区列表字段与发帖/删帖后排序投影。未运行全量回归或竞态检查 |
| HTTP 结果核验 | 独立于施压阶段运行 82 次 GET；核对时间/热度排序、5 个社区范围、前 20 页、末页、空页、总数、每页数量及全部列表字段；ID 顺序与 Redis 一致，字段与 MySQL 一致，退出码 0；[脚本](./output/results/posts-time-20261011/check-list.py)、[日志](./output/results/posts-time-20261011/correctness.log) |
| 报告与台账 | 原生 k6 离线生成对比 HTML，包含四档 p95 柱状图、三轮结果和原始报告链接；[报告脚本](./output/results/posts-time-20261011/report.js)、[生成日志](./output/results/posts-time-20261011/report.log)。台账新增新版 8 轮，共 100 轮，历史记录保留；没有复制历史单轮伪造三轮，也没有调用要求旧版三轮的 `loadtest-compare` |
| 数据恢复 | reset 与后续 HTTP 核验退出码 0；SQL 数量 200 / 5 / 2000 / 20000 / 20000、Outbox 0，Redis DB13 时间/热度全局索引各 2000。普通 sandbox 的 `ss` 无法打开 netlink，改读 `/proc/net/tcp{,6}` 确认 18080 无监听；[核验日志](./output/results/posts-time-20261011/restored-state.log) |

记录的环境一致不代表实时资源负载一致；历史与新版单轮差异仍含缓存、调度及同机竞争的波动。本次没有 SQL profile、连接池等待或完整资源采样，不能把降幅全部作为已隔离的 JOIN 因果收益，也不能断言第一轮丢弃的具体根因。未覆盖生产数据规模、混合流量、跨机网络或长时间运行。

## 业务流程混合负载验证（2026-10-11）

**预期 1000 HTTP RPS 的一轮混合负载未通过：实际成功 866.75 RPS，10 个接口 p95 全部超过 500ms，丢弃 17439 个完整流程，评论列表有一次 5 秒超时；Outbox 判定为“需确认”。** 这是业务流程同时运行的实测，不是把单接口吞吐相加，也不是生产容量或优化收益。

[正式逐接口 HTML](./output/results/20261011-233145011200053-current-mixed-1000/report.html)、[逐接口 CSV](./output/results/20261011-233145011200053-current-mixed-1000/endpoints.csv)、[流程 CSV](./output/results/20261011-233145011200053-current-mixed-1000/journeys.csv)、[原始摘要](./output/results/20261011-233145011200053-current-mixed-1000/summary.json)、[窗口分析](./output/results/20261011-233145011200053-current-mixed-1000/analysis.json)。全量台账 [records.csv](./records.csv) 保留 103 轮；新增 [mixed-records.csv](./mixed-records.csv) 保存冒烟与正式两轮、共 20 条逐接口数据。

### 业务模型与执行条件

用户确认后新增 `make loadtest-mixed` 和 `tests/load/mixed.js`，扩展共享 k6 入口、Shell 编排和原生离线报告；没有修改业务 Go 代码、密码哈希成本、连接池、Worker 或数据库配置。服务为 `20f0b148107f481d735a7b37518651a804af296f`，程序指纹 `919dbc6f821eacd59e97832db1675028034497d383ef468ae670153c2890d783`；测量指纹 `2c42729682dadceb386b2de953ca4c1f45aa5aec7a11b5f2c0eb55d57130488e`，快照指纹保持 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`。离线报告的小失败率显示精度调整不影响施压指纹。

环境沿用本机 i9-13900HX、6 核、约 4 GiB 内存、WSL2；Go 1.27.0、k6 2.3.0、MySQL 8.4.9、Redis 7.4.9。服务与施压器、数据库共享硬件。独立 MySQL `bluebell_k6_verify`、Redis DB13、端口 18080，每轮先恢复 200 用户 / 5 社区 / 2000 帖子 / 20000 评论 / 20000 投票，连接池 10，日志 info/json。数据在施压过程中随注册、发帖和评论增长。

| 用户流程 | 预期次数/秒 | 请求顺序 |
| --- | ---: | --- |
| 游客浏览 | 180 | 列表 → 详情 → 评论列表 |
| 浏览并投票 | 60 | 列表 → 详情 → 评论列表 → 当前投票状态 → 投票 |
| 发表评论 | 20 | 详情 → 评论列表 → 发表评论或同帖回复 → 评论列表回读 |
| 发帖 | 10 | 登录 → 发帖 → 新帖详情 → 时间列表 |
| 老用户登录 | 30 | 登录 |
| 新用户注册 | 5 | 注册 → 新账号登录 |

完整执行时预期 1000 HTTP RPS；实际请求数另行统计。列表按 3:1 查询时间/热榜，80% 访问前三页，其余访问第 4–20 页；浏览完列表后 80% 进入准备阶段固定的 20 篇热门收藏/分享链接，20% 点击响应中的帖子，详情、评论与投票均保持同帖关联。稳定阶段记录 104887 次热点选择、26050 次列表点击，前者约 80.1%；该计数仅覆盖游客和投票流程选帖，不包含发表评论的热点选择。发表评论也按 80% 选择热点，25% 尝试同帖回复。请求间思考时间 0.2–0.8 秒；评论约 600 字节，帖子正文 4 KiB。比例与热点分布是业务假设，没有真实流量日志支撑。

准备阶段为 200 个基线账号真实登录，不计入稳定指标；发帖流程重新登录，注册流程使用新账号登录。账号可能被多个并发流程复用。`VUS=755` 按流程到达率比例向上取整，实际分配 758 VU；执行槽位不表示 758 个独立账号。使用开放到达率，响应变慢不会主动降低计划流程数；槽位满时记录丢弃。参考 [k6 到达率执行器](https://grafana.com/docs/k6/latest/using-k6/scenarios/executors/ramping-arrival-rate/) 和 [VU 分配](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/arrival-rate-vu-allocation/)。

准确命令与原始目录：

```bash
make loadtest-mixed RATE=100 RAMP_SECONDS=0 HOLD_SECONDS=20 VUS=150
make loadtest-mixed RATE=1000 RAMP_SECONDS=30 HOLD_SECONDS=600 VUS=755
make loadtest ENDPOINT=post RATE=1 RAMP_SECONDS=0 HOLD_SECONDS=10 VUS=10
make loadtest-reset
```

冒烟为 [20261011-233009505802701-current-mixed-100](./output/results/20261011-233009505802701-current-mixed-100/report.html)：10 个接口均达标，608 个流程均完成，1920 次稳定 HTTP 请求、零失败与丢弃，成功 96.00 RPS；直接从零启动和流程边界使短窗口请求比例有偏差，后台容量未评估。正式轮为 [20261011-233145011200053-current-mixed-1000](./output/results/20261011-233145011200053-current-mixed-1000/report.html)，北京时间稳定窗口 **23:32:25.923–23:42:25.923**；k6/编排退出 99、make 退出 2，全部原始证据保留，没有追加更高负载或挑选通过样本。

### 正式逐接口验收

每个接口要求 p95 < 500ms、HTTP 与业务失败率 < 1%；p99 为观测值。请求以开始时间划分稳定窗口，窗口后新发出的收尾请求不进入 HTTP 数字。以下 RPS 为业务成功数 / 600 秒：

| 接口 | 预期 RPS | 成功 RPS | p95 ms | p99 ms | HTTP / 业务失败率 | 验收 |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| 时间列表 | 190 | 172.51 | 539.05 | 858.30 | 0 / 0 | 延迟未通过 |
| 热榜 | 60 | 54.58 | 539.50 | 873.86 | 0 / 0 | 延迟未通过 |
| 帖子详情 | 270 | 240.87 | 874.69 | 1283.85 | 0 / 0 | 延迟未通过 |
| 评论列表 | 280 | 245.75 | 1407.45 | 1918.93 | 0.000678% / 0.000678% | 延迟未通过 |
| 当前投票状态 | 60 | 40.82 | 862.50 | 1278.44 | 0 / 0 | 延迟未通过 |
| 投票 | 60 | 40.81 | 550.42 | 873.35 | 0 / 0 | 延迟未通过 |
| 发表评论 | 20 | 13.84 | 929.43 | 1377.01 | 0 / 0 | 延迟未通过 |
| 发帖 | 10 | 8.80 | 869.87 | 1242.79 | 0 / 0 | 延迟未通过 |
| 登录 | 45 | 43.79 | 610.27 | 937.91 | 0 / 0 | 延迟未通过 |
| 注册 | 5 | 5.00 | 943.62 | 1355.38 | 0 / 0 | 延迟未通过 |

稳定阶段共 **520049 次请求、520048 次业务成功**，成功 866.75 RPS；总体 p95 1054.56ms、p99 1566.94ms。唯一失败是游客流程评论列表在 23:34:27 超时，k6 `error_code=1050`、无 HTTP 响应；总体 HTTP / 业务失败率约 0.000192%。失败率虽然符合阈值，延迟与丢弃仍导致整轮失败。报告对小的非零失败率保留 6 位小数，避免显示为 0.00%。

### 流程丢弃与后台

| 流程 | 分配 VU | 稳定开始 / 成功完成 | 丢弃流程 |
| --- | ---: | ---: | ---: |
| 游客浏览 | 446 | 106466 / 106465 | 1534 |
| 浏览并投票 | 149 | 24509 / 24509 | 11491 |
| 发表评论 | 50 | 8308 / 8308 | 3692 |
| 发帖 | 25 | 5277 / 5277 | 722 |
| 老用户登录 | 75 | 18001 / 18001 | 0 |
| 新用户注册 | 13 | 3000 / 3000 | 0 |

总计稳定开始 165561 个流程，成功完成 165560 个。丢弃 17439 的单位是**完整流程**，指标包含升速阶段，不能伪称 17439 个 HTTP 请求或分配给某个接口；超时终止依赖请求，收尾阶段也会改变实际请求比例。k6 日志有四个流程的槽位用满警告。本轮不能声明完整承载 1000 RPS，也不能把此档或 866.75 RPS 当作精确后端上限；增加 VU 是否能消除丢弃需要另测，且不能用增加 VU 代替逐接口延迟验收。

Outbox 每次 SQL 后等待 2 秒，正式窗口内得到 **265 个样本**，查询和同机调度使实际间隔略长于 2 秒。待处理峰值 **130 条**，最老等待峰值 **2.763 秒**，重试数为 0。首/末三分之一待处理中位数 **33 → 45.5**（增长 12.5），年龄中位数 **0.665 → 1.033 秒**（增长 0.368）；不满足“数量 > 10 且年龄 > 1 秒”的持续积压规则，也不满足始终等待 ≤ 1 秒的通过规则，因此后台为**需确认，未通过验收**。停压后约 **4.295 秒**首次采样到归零，最终仍为零；这只是恢复观测，不是逐事件完成延迟或施压期间可持续的证据。

停压后数据库为 3275 用户 / 5 社区 / 7427 帖子 / 28608 评论 / 27203 投票 / 0 Outbox；[停压数据量](./output/results/20261011-233145011200053-current-mixed-1000/postflight-counts.tsv) 已保存，再恢复基线。不能拿停压数据规模与原单接口固定规模算优化收益。

### 验证、收尾与下一步

| 检查 | 结果 / 证据 |
| --- | --- |
| 混合链路冒烟 | 退出 0；[原始驱动日志](./output/results/20261011-233009505802701-current-mixed-100/driver.log) |
| 正式负载 | 完整 30 + 600 秒；原始退出 99、make 退出 2；[驱动日志](./output/results/20261011-233145011200053-current-mixed-1000/driver.log)、[make 退出码](./output/results/20261011-233145011200053-current-mixed-1000/make-exit-code.txt) |
| 报告判定回归 | 合成输入验证单接口失败、流程未完成、缺失混合数据及运行异常不能被总体 HTTP 达标掩盖；CSV 覆盖 10 接口 / 6 流程；[检查脚本](./output/results/20261011-233145011200053-current-mixed-1000/report-check.cjs)、[日志](./output/results/20261011-233145011200053-current-mixed-1000/report-check.log)。合成数据只作报告正确性校验，不计入性能台账 |
| 原单接口兼容性 | 详情 1 RPS / 10 秒退出 0，p95 4.89ms，无失败与丢弃；[单轮报告](./output/results/20261011-234413857208933-current-post-1/report.html)、[日志](./output/results/20261011-233145011200053-current-mixed-1000/single-regression.log) |
| 脚本与数据合计 | Bash / 三个 JS 语法、`git diff --check` 通过；逐接口请求数合计 520049、逐流程丢弃合计 17439 与原始总体一致 |
| 数据恢复 | `make loadtest-reset` 退出 0；[恢复日志](./output/results/20261011-233145011200053-current-mixed-1000/reset.log)；独立库恢复 200 / 5 / 2000 / 20000 / 20000、Outbox 0，Redis DB13 时间排行榜 2000 条、18080 无监听；[SQL 核对](./output/results/20261011-233145011200053-current-mixed-1000/restored-counts.tsv)、[Redis 核对](./output/results/20261011-233145011200053-current-mixed-1000/restored-redis.txt)、[端口核对](./output/results/20261011-233145011200053-current-mixed-1000/restored-listeners.txt) |

改动与指标文档已同步到根 README、压测 README、本节和两份台账。没有改动业务 Go 代码，本次未重新运行 Go 功能测试，不将历史缓存结果算成本次回归。

下一步先验证相同比例下较低负载（例如 500 RPS）的混合档位，并根据实际流程时长预分配 VU；若要定位本档延迟，分开采集服务 / k6 CPU、MySQL 连接池等待和评论查询耗时，再决定优化，不能仅凭评论 p95 最高认定评论 SQL 是根因。评论读取包含查帖、COUNT、分页和作者查询，认证仍使用 bcrypt，但本轮没有 profile 或查询计时来隔离这些成本。本次只完成已确认的一轮正式验证，未追加优化、降档或重复施压。

未覆盖生产数据规模、跨机网络、长期 soak、真实流量日志比例、删除/登出/刷新令牌和社区导航；评论回读仅验证总数增长，不证明新增 ID 在当前页，发帖不要求异步索引立即可见。原始 output 被 Git 忽略，迁移或分享时须保留完整轮次目录。

查询语义仍有一个未覆盖边界：新版使用 INNER JOIN，作者或社区记录缺失时会省略该帖子；旧版按名称 map 构建响应，会保留帖子并给出空名称，而总数继续来自 Redis。本次固定数据集的关联完整，现有回归与 82 次 GET 不覆盖关联缺失或软删除后 Redis 残留 ID，不能据此宣称所有边界行为不变。此处记录代码差异，没有修改用户的实现。

原始 HTML、JSON、日志和临时核验脚本位于被 Git 忽略的 output 目录；已跟踪文档及 CSV 保留结论，迁移或分享时须另行保留报告和所引用的历史、新版轮次目录。

## 动态 VU 校准与混合负载复测（2026-10-12）

**800 HTTP RPS 已通过一次 30 秒升速 + 600 秒稳定验证**：[正式报告](./output/results/20261012-012958210676601-current-mixed-800/report.html)。907 VU，479999 个请求全部成功、146399 个已开始流程全部完成，零 HTTP/业务错误、零丢弃；总体 p95 73.01ms，十接口最高 p95 为注册的 143.36ms。Outbox 峰值 39 条 / 0.587 秒，无重试，未见持续积压。这是当前条件下的已验证容量下界，不能当作生产容量或精确上限。

1000 RPS 在完整施压的 60 秒轮次中，评论列表 p95 534.27ms、p99 1022.13ms，超出 p95 <500ms 标准；其余九接口达标，Outbox 需确认。该轮成功速率 992.37 RPS、零丢弃，施压与服务失败已分开判断。此结果保留，不用后续短测通过覆盖，也不将 992.37 RPS 当作后端吞吐上限。

### 改动范围与条件

仅修改压测脚本、离线报告与文档；没有改业务代码、连接池、Worker、索引或日志配置。沿用提交 `20f0b148107f481d735a7b37518651a804af296f`、MySQL 连接池 10、Worker 并发 4、原混合流程/思考时间/热点分页和原固定快照。服务二进制指纹 `919dbc6f821eacd59e97832db1675028034497d383ef468ae670153c2890d783`；快照指纹 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`。仍在同一 WSL2 / 6 逻辑核 / 约 3.83 GiB 的隔离库 `bluebell_k6_verify`、Redis DB13、端口 18080 上测量，服务、k6 和数据库共享硬件。每轮恢复数据，初始化与真实登录不计时。

- 成功与失败流程分别记录完整耗时、样本数；零样本为空，流程占用按稳定窗口裁剪。初始配额使用当前档流程率和成功均值；少于 30 个成功样本采用启动预算。100 RPS 时注册约 0.5 次/秒，p99 只作为参考，从未用于外推所有档位配额。
- 每档短校准、60 秒正式轮和 600 秒复测共享三次调整额度；实际丢弃、开始数不足或平均占用超过 75% 时反馈增加受影响流程配额。同档丢弃还使用本档已观测最长成功流程耗时作为保守预算，不外推到其他档位。正式轮配额固定，调整后恢复数据并重新验证。
- 施压完整性、资源保护、十接口性能和 Outbox 独立判定；丢弃单位是流程。采集服务、k6、MySQL 与主机资源；MySQL 从容器内 `/proc/1` 读取，防止 WSL/Docker PID 命名空间误配。判定细则见 [使用说明](./README.md#逐档动态校准与独立判定)。

摘要保留 `vus` 原参数与 `mixed.allocation` 实际映射，CSV 对应 `vus` 和 `mixed_allocation`；显式映射覆盖原比例配额，实际总数以 `mixed.allocated_vus` / 流程表为准。校准和诊断轮不参与正式容量区间。脚本在完善反馈时发生变化，各轮真实指纹完整保存；这些轮次不是业务优化 A/B，不计算收益百分比。

### 全部正式轮次

[本次批次总览](./output/results/20261012-012846-mixed-stable-feedback/report.html)包含已正常归档的本次校准、诊断和正式样本；[完整索引](./output/results/index.html)、[逐轮台账](./records.csv)、[逐接口台账](./mixed-records.csv)保留全部历史。

| 轮次时间 | 目标 RPS / 稳定秒 | 实际 VU | 成功 RPS | 丢弃流程 | 施压 | 服务 HTTP；最高接口 p95 ms | Outbox |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| [002537](./output/results/20261012-002537423756211-current-mixed-400/report.html) | 400 / 60 | 232 | 399.82 | 0 | 完整施压 | 达标；signup 69.31 | 未见持续积压 |
| [002822](./output/results/20261012-002822945689231-current-mixed-600/report.html) | 600 / 60 | 331 | 599.68 | 0 | 完整施压 | 达标；signup 78.57 | 未见持续积压 |
| [003108](./output/results/20261012-003108026816064-current-mixed-800/report.html) | 800 / 60 | 430 | 799.22 | 0 | 完整施压 | 达标；signup 88.45 | 未见持续积压 |
| [003351](./output/results/20261012-003351978534811-current-mixed-1000/report.html) | 1000 / 60 | 534 | 995.83 | 28 | 施压未校准 | 达标；comments 187.55 | 未见持续积压 |
| [004505](./output/results/20261012-004505310847544-current-mixed-400/report.html) | 400 / 60 | 230 | 399.72 | 0 | 完整施压 | 达标；signup 70.46 | 未见持续积压 |
| [004751](./output/results/20261012-004751044681887-current-mixed-600/report.html) | 600 / 60 | 329 | 599.37 | 0 | 完整施压 | 达标；signup 74.46 | 未见持续积压 |
| [005035](./output/results/20261012-005035624993077-current-mixed-800/report.html) | 800 / 60 | 430 | 799.70 | 0 | 完整施压 | 达标；signup 96.98 | 未见持续积压 |
| [005425](./output/results/20261012-005425854356986-current-mixed-1000/report.html) | 1000 / 60 | 551 | 995.60 | 28 | 施压未校准 | 达标；comments 239.01 | 未见持续积压 |
| [005709](./output/results/20261012-005709224782620-current-mixed-1000/report.html) | 1000 / 60 | 639 | 988.57 | 136 | 施压未校准 | 达标；comments 444.31 | 需确认 |
| [005953](./output/results/20261012-005953599732178-current-mixed-1000/report.html) | 1000 / 60 | 790 | 992.37 | 0 | 完整施压 | 未达标；comments 534.27 | 需确认 |
| [010238](./output/results/20261012-010238240600777-current-mixed-800/report.html) | 800 / 600 | 430 | 798.72 | 225 | 施压未校准 | 达标；signup 186.54 | 未见持续积压 |
| [012958](./output/results/20261012-012958210676601-current-mixed-800/report.html) | 800 / 600 | 907 | 800.00 | 0 | 完整施压 | 达标；signup 143.36 | 未见持续积压 |

1000 RPS 前两次正式反馈失败和 800 RPS 首次长测失败属于施压未校准，不能证明完整承载目标流量。800 首次长测 430 VU 时丢弃 225 个流程，十接口仍达标；反馈到本档已观测耗时后，907 VU 的校准与长测均通过。增加 VU 是修正施压完整性，不能称为服务优化收益。

### 800 RPS / 600 秒逐接口与流程

所有接口 HTTP/业务失败率均为 0；p99 展示为参考，验收依据是各接口 p95、错误率、流程完成和 Outbox。

| 接口 | 预期 RPS | 成功 RPS | p95 ms | p99 ms |
| --- | ---: | ---: | ---: | ---: |
| posts_time | 152.00 | 152.00 | 28.52 | 58.50 |
| posts_hot | 48.00 | 48.00 | 28.73 | 58.22 |
| post | 216.00 | 216.01 | 47.07 | 91.43 |
| comments | 224.00 | 224.00 | 91.19 | 156.84 |
| get_vote | 48.00 | 47.99 | 45.79 | 88.38 |
| vote | 48.00 | 47.99 | 50.99 | 81.71 |
| create_comment | 16.00 | 16.01 | 71.81 | 120.80 |
| create_post | 8.00 | 8.00 | 67.48 | 113.75 |
| login | 36.00 | 36.00 | 109.22 | 142.10 |
| signup | 4.00 | 4.00 | 143.36 | 182.64 |

| 流程 | VU | 开始 / 完成 | 成功耗时样本 | 成功耗时均值 ms | 平均占用率 |
| --- | ---: | ---: | ---: | ---: | ---: |
| guest | 530 | 86400 / 86400 | 86400 | 1057.15 | 28.72% |
| member | 242 | 28800 / 28800 | 28800 | 2100.07 | 41.65% |
| commenter | 77 | 9600 / 9600 | 9600 | 1612.98 | 33.52% |
| publisher | 24 | 4799 / 4799 | 4799 | 1625.31 | 54.17% |
| returning | 25 | 14400 / 14400 | 14400 | 69.17 | 6.64% |
| newcomer | 9 | 2400 / 2400 | 2400 | 661.99 | 29.42% |

发帖流程比窗口期望少 1 条开始，属于报告明确允许的单条边界计时差；已开始流程全部完成。逐接口请求按发出时间归属窗口，因此不能由流程次数简单推算窗口内请求总量。

每个资源来源有 270 个稳定样本：服务 CPU 平均 2.161 核、峰值 2.245 核，k6 平均 0.342 核、峰值 0.547 核，MySQL 平均 1.453 核、峰值 1.872 核；主机忙碌平均 73.81%、峰值 82.21%。k6 峰值 RSS 1059.84 MiB，可用内存最低 1424.89 MiB，未触发施压保护。这些资源数据描述本轮环境，不是具体根因证据。停压时数量为 2660 用户、5 社区、6919 帖子、29839 评论、28661 投票、Outbox 0，单帖最多 432 条评论；仅用于复现数据增长条件。

### 最小必要诊断与运行异常

保留 1000 RPS 的评论列表超标轮后，以相同流程和 790 VU 缩短窗口，仅验证 `comments.p95 >=500ms` 是否重现，没有先按接口排序断言 SQL 根因。结果：[20 秒无升速](./output/results/20261012-011900838740650-diagnostic-mixed-1000/report.html)评论 p95 41.96ms；[10 秒升速 +20 秒](./output/results/20261012-012226641828076-diagnostic-mixed-1000/report.html)58.07ms；[10 秒升速 +40 秒](./output/results/20261012-012457634606227-diagnostic-mixed-1000/report.html)56.85ms。第二轮施压未校准，其余两轮完整施压；20 秒窗口的 Outbox 不足以判定容量。三次均未稳定复现超标，根因未确认，因此未追加 SQL/Go 插桩、profile 或业务修复。不能据此撤销原失败，也不能声称 1000 RPS 已稳定通过。

- `make loadtest-mixed-probe` 第一批运行中，1000 RPS /60 秒丢弃 28 个流程却被编排直接选低档长测，发现缺少正式轮反馈。修正编排前主动终止第一批：make 退出 2、驱动退出 143；`20261012-003637918782539-current-mixed-800` 长测未完整结束，没有补写退出码、不纳入台账。第一批此前完整正式样本仍保留。
- 最初 100 RPS /20 秒指标冒烟的 k6 退出 0，但运行期间修改了正在读取的 Shell 文件导致驱动解析异常、make 退出 2；只验证指标，不算容量。日志已归档。
- 第二批完整探测在 800 首次长测施压失败后退出 2，环境恢复成功。随后复用该档失败证据和剩余三次额度，只恢复 800 同档反馈；第一次调整到 907 VU 后短校准及 600 秒长测通过，续测驱动退出 0。续测脚本和全部日志保留，未重写旧样本。
- 曾尝试完整数据库诊断导出，被自动审批拒绝；已撤去导出动作，改为只读各表数量与单帖最大评论数，没有产生诊断 SQL 导出。
- 三处 GORM 慢 SQL 日志含用户 bcrypt 哈希，已原位脱敏并保留 SQL 结构、耗时和测量数据；新增压测收尾哈希/JWT 脱敏规则。该规则在最终测量后加入，不影响施压与指标采样。测量版本源码归档在批次 `measurement-source`，初始化凭据已脱敏；原始指纹仍以各轮摘要为准，不用脱敏文件重新计算替代指纹。

### 验证与归档

执行入口为 `make loadtest-mixed-probe`；最终同档续测使用[归档脚本](./output/results/20261012-012846-mixed-stable-feedback/bluebell-mixed-stable-feedback.sh)，输入为 800 首次长测摘要，沿用三次调整额度。其目录还保存两批探测、短复现、冒烟、恢复与单接口回归的完整驱动日志、退出码、校准决定和测量版本源码。

| 验证 | 结果与限制 |
| --- | --- |
| 配额与独立判定规则 | 原生模块检查通过：逐档、低样本启动、p99 无影响、同档丢弃反馈、施压/服务分判、缺样与资源保护；合成样本不进入性能台账 |
| 编排反馈与预算 | Shell 桩验证通过：短测/长测反馈成功、服务失败停止、资源不足停止、三次上限与校准/正式轮共用额度 |
| 离线报告 | 逐接口失败、流程未完成、数据缺失、运行异常合成检查通过；本次批次与全量 HTML/CSV 正常重建 |
| 参数/语法/补丁 | k6 inspect 完整显式配额通过、错误映射拒绝；Bash/JS 语法、日志脱敏合成检查和 `git diff --check` 通过 |
| 单接口兼容 | [帖子详情 1 RPS /10 秒](./output/results/20261012-020046370406421-current-post-1/report.html)，k6/驱动退出 0，成功 0.90 RPS、p95 5.07ms，无错误或丢弃；这是冒烟而非容量 |
| 历史数据保护 | 原 103 条逐轮记录、20 条混合接口记录的原字段逐项保持不变；原始摘要、标签、退出码未篡改 |
| 环境恢复 | SQL 为 200 用户、5 社区、2000 帖子、20000 评论、20000 投票、Outbox 0；Redis 时间榜 2000；18080 无监听。核对结果在批次目录 |

没有 Go 业务改动，未重跑 Go 功能/竞态测试，也不引用此前测试作为本次验证。原始 output 目录被 Git 忽略，迁移时需另行复制；CSV 和本文不能替代原始证据。


## 评论列表查询计划与连接池诊断（2026-10-12）

**已确认评论 COUNT / 分页查询的 `index_merge` 扫描放大，以及共享 MySQL 连接池排队造成的尾延迟放大。** 在原参数下重新复现超标；只改变评论查询使用的索引，两轮 p95 为 24.91 / 30.46ms，回退原计划后 p95 又达到 1026.68ms。正式业务代码、配置、表结构及索引均未修改；这是一组诊断证据，不是已合入的优化收益或 1000 RPS /10 分钟验收。

[批次报告](./output/results/comments-diagnosis-20261012/report.html)、[分析 JSON](./output/results/comments-diagnosis-20261012/analysis.json)、[执行计划](./output/results/comments-diagnosis-20261012/query-plans.txt)、[评论 COUNT 随时间的变化](./output/results/comments-diagnosis-20261012/comment-count-growth.json)。本次用独立 Python 编排只读 MySQL/主机采样与离线分析，HTTP 施压及指标仍由原生 k6 执行。采样、分析脚本、各轮 CPU/block profile、逐 SQL 类别计时和连接池原始采样都在该目录；原生 k6 图表仍在各轮目录。

### 复现条件与完整结果

沿用原失败轮的全部业务参数：1000 预期 HTTP RPS、10 秒升速、60 秒稳定、`VUS=755`、显式分配 `guest/member/commenter/publisher/returning/newcomer=423/235/80/29/13/10`，实际共 790 VU；原 MySQL max_open/max_idle=10/10。保持混合流程、比例、随机热点/分页、思考时间、正文大小和原快照，不调整施压配额。快照 SHA256 仍为 `97f5deb9b304e744acb22b1c4807758b54baf75aa09b535e9745905c88ec25dc`，测量脚本 SHA256 为 `f2a70d76a99983f9bb899eddb0bed259ea032a090b23a18aa1211b09ee5b6d5d`；当前提交 `4d03f4b` 相对原业务提交 `20f0b14` 仅有压测/文档变化。仍为本机 WSL2、6 逻辑核、约 3.83 GiB，独立库 `bluebell_k6_verify`、Redis DB13、端口 18080。

```bash
make loadtest-mixed RATE=1000 RAMP_SECONDS=10 HOLD_SECONDS=60 VUS=755 LABEL=diagnostic \
  MIXED_VUS='{"guest":423,"member":235,"commenter":80,"publisher":29,"returning":13,"newcomer":10}'
```

共 13 轮完整稳定窗口：原业务/配置 7 轮（3 轮无插桩、4 轮只采样），仅改连接池上限 2 轮，仅改驱动往返方式 2 轮，仅改评论索引选择 2 轮。每轮重新恢复快照，标签均为 `diagnostic`；阈值失败正常归档，没有挑选通过轮替换失败轮。正式延迟验收是 p95 <500ms，p99 只作参考。

| 轮次 | 唯一实验变量 | 评论 p95 / p99 ms | 成功 RPS | 丢弃流程 | 稳定窗口累计池等待 s | Outbox；峰值条数 / 最老秒 |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| [baseline-1](./output/results/20261012-162811453909424-diagnostic-mixed-1000/report.html) | 原代码，无插桩 | 843.24 / 1504.46 | 981.38 | 192 | 未采集 | 需确认；147 / 2.309 |
| [baseline-2](./output/results/20261012-163251944354502-diagnostic-mixed-1000/report.html) | 原代码，无插桩 | 356.95 / 653.14 | 994.77 | 0 | 未采集 | 未见持续积压；48 / 0.795 |
| [profile-1](./output/results/20261012-163558542830534-diagnostic-mixed-1000/report.html) | 原代码，仅采样 | 299.63 / 666.19 | 994.02 | 0 | 1334.66 | 未见持续积压；36 / 0.627 |
| [profile-2](./output/results/20261012-163915288055204-diagnostic-mixed-1000/report.html) | 原代码，仅采样 | 159.20 / 345.64 | 997.33 | 0 | 852.03 | 未见持续积压；37 / 0.670 |
| [pool20-1](./output/results/20261012-164204109709204-diagnostic-mixed-1000/report.html) | 仅 max_open=20 | 81.08 / 158.92 | 999.33 | 0 | 246.11 | 未见持续积压；34 / 0.577 |
| [pool10-return](./output/results/20261012-164447303312942-diagnostic-mixed-1000/report.html) | 恢复连接池 10，仅采样 | 172.14 / 303.10 | 996.57 | 0 | 1010.33 | 未见持续积压；31 / 0.502 |
| [pool20-2](./output/results/20261012-164714216458470-diagnostic-mixed-1000/report.html) | 仅 max_open=20 | 376.43 / 689.44 | 995.37 | 0 | 1717.30 | 未见持续积压；53 / 0.901 |
| [baseline-final](./output/results/20261012-164937118434903-diagnostic-mixed-1000/report.html) | 原代码，无插桩 | 446.48 / 908.42 | 993.33 | 1 | 未采集 | 需确认；81 / 1.364 |
| [interpolate-1](./output/results/20261012-165232986018806-diagnostic-mixed-1000/report.html) | 仅减少 Prepare/Close 往返 | 59.19 / 154.88 | 998.30 | 0 | 237.47 | 未见持续积压；34 / 0.513 |
| [interpolate-2](./output/results/20261012-165444331210746-diagnostic-mixed-1000/report.html) | 仅减少 Prepare/Close 往返 | 28.05 / 84.74 | 998.87 | 0 | 79.86 | 未见持续积压；42 / 0.546 |
| [force-index-1](./output/results/20261012-165726734533565-diagnostic-mixed-1000/report.html) | 仅指定评论 post_id 索引 | 24.91 / 57.26 | 999.55 | 0 | 77.63 | 未见持续积压；36 / 0.504 |
| [force-index-2](./output/results/20261012-165941984457604-diagnostic-mixed-1000/report.html) | 仅指定评论 post_id 索引 | 30.46 / 74.33 | 999.57 | 0 | 120.08 | 未见持续积压；33 / 0.442 |
| [plan-return](./output/results/20261012-170204000726170-diagnostic-mixed-1000/report.html) | 回退原查询计划，仅采样 | 1026.68 / 1602.23 | 979.73 | 204 | 5988.57 | 需确认；114 / 1.716 |

首轮与最后的原计划回退轮均复现评论超标，但分别丢弃 192 / 204 个流程，不能称为完整承载 1000 RPS。无插桩收尾轮丢弃 1 个流程、Outbox 需确认，也未通过。两轮评论索引探针均完整施压，十接口全部达标，流程全部成功完成、零 HTTP/业务失败、零丢弃。原 534.27ms /零丢弃的历史失败仍保留；本次没有覆盖其逐请求内部耗时，也没有执行 1000 RPS /600 秒。

### 查询计划为什么会随测试变慢

恢复基线后，2000 篇帖子中 **1000 篇没有评论，另 1000 篇各有 20 条评论**，不是每篇均匀分配 10 条。代表帖 2126 在基线中存在但评论数为 0；复跑后它有 63 条评论。零评论时服务在 COUNT 后返回，只执行查帖和 COUNT；出现评论后增加分页与作者查询，变为四次串行数据库操作。

[恢复后的分布](./output/results/comments-diagnosis-20261012/baseline-comment-distribution.tsv)与[原计划对照](./output/results/comments-diagnosis-20261012/restored-counts-and-plan.tsv)显示：帖 2126 零评论时使用 `idx_comments_post_id` 查找，扫描 0 行；63 条评论时，原 COUNT 与分页都选择了 `idx_comments_post_id` 和 `idx_comments_deleted_at` 的索引交集。

| 同一帖 63 条评论，停压只读 EXPLAIN ANALYZE | 原计划 | 仅指定已有 post_id 索引 |
| --- | --- | --- |
| COUNT | `index_merge`：post_id 分支 63 行，deleted_at 分支 **21280 行**；2.84ms | post_id 查找 63 行；0.0749ms |
| 分页 | 同样扫描 21280 条 deleted_at 索引记录，再排序；约 2.96ms | 读取该帖 63 行后排序；约 0.0622ms |

绝大多数评论未软删除，`deleted_at IS NULL` 分支接近全表；其交集访问成本远大于只取该帖记录。这里不是缺失 `post_id` 索引，而是已有单列索引组合被优化器选成了更昂贵的访问路径。计划中的耗时是停压后单次测量，不是接口 p95，不能用它代替高并发结果。

驱动插值探针使真实 SELECT 能在 digest 统计中单独识别，捕获了随评论增长的过程：

| 稳定窗口秒 | COUNT 平均检查行数 | MySQL COUNT 平均执行 ms |
| ---: | ---: | ---: |
| 0–10 | 166.69 | 0.133 |
| 10–20 | 1419.60 | 0.331 |
| 20–30 | 10348.83 | 1.601 |
| 30–40 | 16690.95 | 2.623 |
| 40–50 | 16700.31 | 2.822 |
| 50–60 | 17035.73 | 3.209 |

此表来自 `interpolate-2`，不冒充原驱动失败轮的直接采样。它说明保持相同到达率也不代表数据库工作量固定：新评论让更多请求从两次查询变为四次，并让 COUNT/分页访问路径出现扫描放大。20/40 秒短测未复现，不能排除完整窗口后段的这一变化。

### 排队与单变量反证

- **排队已直接测到**：原计划回退轮稳定窗口中连接池 67.5% 的 100ms 采样占满，累计等待 5988.57 秒、79476 次等待。查帖、COUNT、分页、作者读取各阶段 Go 侧 p95 为 261.88 / 290.57 / 294.02 / 296.34ms；这些耗时包含取连接等待，不能解读成 MySQL 单条 SQL 执行时间。
- **阻塞调用链一致**：[回退轮 block profile](./output/results/comments-diagnosis-20261012/plan-return/block-top.txt)中 `database/sql.(*DB).conn` 累计约 6354.04 秒，其中 `CommentService.List` 约 3041.99 秒。profile 覆盖准备、升速、稳定及恢复，与上面仅稳定窗口的 DBStats 范围不同。
- **仅扩池不足以解决**：max_open=20、max_idle 保持 10 的两轮评论 p95 为 81.08 / 376.43ms，等待为 246.11 / 1717.30 秒。结果波动，不能断言把池改成 20 已解决根因，也未独立诊断空闲连接关闭/重建成本。
- **额外往返确有成本**：原驱动每轮约 13.3–13.6 万次 Prepare、Execute、Close；仅 `interpolateParams=true` 后三项均为 0，评论 p95 两轮 59.19 / 28.05ms，池等待 237.47 / 79.86 秒。这是另一个诊断分支，没有与索引探针合并，也没有合入配置。
- **只消除索引交集效果可重复**：保持原驱动、max_open/max_idle=10/10，只在两条评论 SELECT 上指定已有 `idx_comments_post_id`，p95 两轮 24.91 / 30.46ms、池等待 77.63 / 120.08 秒。MySQL Execute 均值为 0.174 / 0.168ms，MySQL 平均 CPU 0.451 / 0.438 核；原计划采样约 0.85–0.90 核。回退原查询后评论再次超标、积压检查再次需确认。

归因链：**评论增长 → 查询次数与索引交集扫描增加 → 数据库连接占用增加 → API / Worker 共享池排队 → 评论四段串行操作放大尾延迟**。这条链有数据分布、实际执行计划、随时间的数据库计数、连接池计时、block profile 和单变量回退证据；同机调度波动仍会影响具体 p95，不能把历史失败轮的每一毫秒全部归到这一项。

### Outbox、其他候选与边界

原计划首轮 Outbox 为 147 条 /2.309 秒、收尾无插桩轮为 81 条 /1.364 秒、回退轮为 114 条 /1.716 秒，均无重试，仍按原规则判“需确认”。回退轮末段相对首段待处理中位数增长 40 条、年龄增长 0.620 秒，未同时达到“持续积压”的两个条件，不能自行改判为持续积压或通过。评论索引探针两轮分别为 36 /33 条、最老 0.504 /0.442 秒，无重试，正式筛查均为“未见持续积压”；这支持读链路释放共享资源有利于 Worker，但没有测逐事件可见延迟或 Worker 各阶段确认吞吐，不能据此断言已排尽所有 Outbox 内部问题。

所有有 MySQL 计数的轮次中 `Innodb_row_lock_waits/time` 增量为 0，未捕获 `data_lock_waits`；`Innodb_buffer_pool_reads` 增量为 0，未见本轮 buffer pool 冷读导致的读盘。COMMIT 平均约 12.9–14.1ms，binlog/redo 文件同步有耗时，是连接占用的一部分；评论索引探针未改持久化配置即可改善，不能单凭 COMMIT 时间把根因改判为刷盘。CPU profile 中 bcrypt/Blowfish 占约 90% 服务 CPU 样本；profile 含准备登录，不能直接当成稳定阶段占比，也没有证据把 bcrypt 单独认定为此次评论超标根因。k6 和内存未触发施压资源保护；Go 进程未显式设置 GOMAXPROCS，CPU 亲和性为 0–5。

连接池累计等待是多请求并发累加，不是一次请求等待了几千秒；100ms 池采样与约 1 秒 MySQL 快照按最近边界计算增量，有边界误差。Go SQL 类别计时按开始时间归窗，包含连接池、网络和结果读取；MySQL Prepare/Execute 汇总不能代表每类 SQL 的 p95。所有短探针都不满足正式三轮 180 秒 A/B，本文不计算优化百分比或宣称容量提升。

### 改动、复跑与恢复

主仓库只更新本节、压测说明及两份诊断台账。诊断代码在被 Git 忽略的 `comments-diagnosis-20261012/source*` 中，未修改主仓库业务代码，也未添加索引、修改数据库优化器开关或持久化设置。[插桩补丁](./output/results/comments-diagnosis-20261012/instrumentation.patch)、[索引探针补丁](./output/results/comments-diagnosis-20261012/force-index.patch)、[驱动探针补丁](./output/results/comments-diagnosis-20261012/interpolate.patch)均保留；SQL 采样不保存参数值，服务日志沿用 bcrypt/JWT 脱敏。

```bash
# 只读采样 + 原参数完整一轮；退出码和原始目录另存于子目录
python3 tests/load/output/results/comments-diagnosis-20261012/collect.py baseline-new

# 复跑原查询的插桩轮；source 为明确标记的诊断副本
python3 tests/load/output/results/comments-diagnosis-20261012/collect.py profile-new \
  SERVER_SOURCE_DIR="$PWD/tests/load/output/results/comments-diagnosis-20261012/source" \
  BLUEBELL_DIAG_DIR="$PWD/tests/load/output/results/comments-diagnosis-20261012/profile-new"

# 分析已归档各轮，不施压
python3 tests/load/output/results/comments-diagnosis-20261012/analyze.py
```

诊断副本、完整条件与计数方式见 [conditions.json](./output/results/comments-diagnosis-20261012/conditions.json)。命令均需本机 Docker 访问权限；最初容器未运行导致一次启动前失败，启动已有 Compose 后才产生有效测量。镜像、Go/k6 版本与每轮实际二进制指纹保存在摘要，不因业务源码一致而改写旧指纹。

验证：13 轮均生成 60 秒完整摘要、十接口/六流程报告；临时 Go 副本实际编译并执行混合链路；分析脚本语法与证据一致性核对、`git diff --check` 通过。没有正式业务修改，未新增回归测试或重跑 Go 功能测试；负载业务断言只作为本次诊断探针的验证，不替代正式优化的功能回归。

`make loadtest-reset` 成功，已用主仓库源码重建服务二进制；独立库恢复 200 用户、5 社区、2000 帖子、20000 评论、20000 投票、Outbox 0；Redis DB13 全站时间榜 2000 条，18080 无监听。[恢复日志](./output/results/comments-diagnosis-20261012/reset.log)与只读核对证据保留。下一步优先验证支持 `post_id + deleted_at` 过滤及分页顺序的联合索引，再决定查询合并或驱动参数；正式修复与 1000 RPS /10 分钟验收尚未执行，已验证容量下界仍为 800 RPS。
