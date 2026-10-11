// 原生 k6 离线报告：不发 HTTP 请求，不安装报告库。
import { generatorAssessment } from './mixed-capacity.js';
const paths = (__ENV.REPORT_FILES || '').split(',').filter(Boolean);
if (!paths.length) throw new Error('REPORT_FILES 必须包含 summary.json 的绝对路径，以逗号分隔');
if (__ENV.REPORT_MODE === 'compare' && __ENV.BEFORE && __ENV.AFTER &&
    [__ENV.BEFORE, __ENV.AFTER].some(value => value.split(',').filter(Boolean).length !== 3)) {
  throw new Error('BEFORE 和 AFTER 必须各包含三个结果目录');
}
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const number = (v, digits = 2) => Number.isFinite(v) ? v.toFixed(digits) : '无数据';
const percent = v => Number.isFinite(v) ? `${number(v * 100,
  v > 0 && v < 0.0001 || v > 0.9999 && v < 1 ? 6 : 2)}%` : '无数据';
const median = a => { const s = [...a].sort((x, y) => x - y); return s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2; };
const written = r => ['create_post', 'vote', 'mixed'].includes(r.endpoint);
const names = { signup: '注册', login: '登录', post: '帖子详情', posts_time: '时间列表', posts_hot: '热榜',
  comments: '评论列表', get_vote: '当前投票状态', create_comment: '发表评论', create_post: '发帖', vote: '投票', mixed: '业务混合负载' };
const journeyNames = { guest: '游客浏览', member: '浏览并投票', commenter: '发表评论', publisher: '发帖', returning: '老用户登录', newcomer: '注册后登录' };
const runs = paths.map((path, index) => {
  const r = JSON.parse(open(path));
  if (r.schema !== 1) throw new Error(`不兼容的历史报告：${path}`);
  // 对照阶段由 BEFORE / AFTER 参数指定，允许复用上一项优化的 after 作为新基线。
  // 只修改离线展示标签，原始摘要与完整台账保持原标签。
  if (__ENV.REPORT_MODE === 'compare') r.label = index < 3 ? 'before' : 'after';
  r.dir = path.slice(0, path.lastIndexOf('/'));
  r.name = r.dir.slice(r.dir.lastIndexOf('/') + 1);
  r.exit = Number(open(`${r.dir}/exit-code.txt`).trim());
  r.success_rps = r.metrics.successes === null ? null : r.metrics.successes / r.hold;
  if (r.endpoint === 'mixed') r.http_pass = r.http_pass && r.mixed?.endpoints?.length === 10 &&
    r.mixed.endpoints.every(e => e.pass) && r.mixed.journeys?.length === 6 &&
    r.mixed.journeys.every(j => j.started > 0 && j.completed === j.started && j.dropped === 0);
  r.valid = [0, 99].includes(r.exit) && r.metrics.requests > 0 && Number.isFinite(r.metrics.p95);
  r.samples = written(r) ? open(`${r.dir}/outbox.csv`).trim().split('\n').slice(1).map(line => {
    const [time, pending, oldest, retries] = line.split(',').map(Number);
    return { time, pending, oldest, retries };
  }).filter(s => Object.values(s).every(Number.isFinite)) : [];
  const start = r.load_start_ms / 1000;
  r.stable = r.samples.filter(s => s.time >= start && s.time < start + r.hold);
  r.outbox_peak = r.stable.length ? Math.max(...r.stable.map(s => s.pending)) : null;
  r.outbox_age = r.stable.length ? Math.max(...r.stable.map(s => s.oldest)) : null;
  r.outbox = '不适用';
  if (written(r)) {
    r.outbox = '需确认';
    if (r.stable.length >= 10 && r.hold >= 30) {
      const n = Math.floor(r.stable.length / 3);
      const first = r.stable.slice(0, n), last = r.stable.slice(-n);
      const pendingGrowth = median(last.map(s => s.pending)) - median(first.map(s => s.pending));
      const ageGrowth = median(last.map(s => s.oldest)) - median(first.map(s => s.oldest));
      r.outbox = r.stable.some(s => s.retries > 0) ? '重试异常' :
        pendingGrowth > 10 && ageGrowth > 1 ? '持续积压' :
          Math.max(...r.stable.map(s => s.oldest)) <= 1 && pendingGrowth <= 10 ? '未见持续积压' : '需确认';
    }
  }
  if (r.endpoint === 'mixed') {
    r.generator = generatorAssessment(r, r.resource_sampling ? open(`${r.dir}/resources.csv`) : '');
    r.service_http_pass = r.mixed?.endpoints?.length === 10 && r.mixed.endpoints.every(e => e.pass) &&
      r.mixed.journeys?.length === 6 && r.mixed.journeys.every(j => j.started > 0 && j.completed === j.started);
    r.service_pass = r.service_http_pass && r.outbox === '未见持续积压';
  }
  r.pass = r.http_pass && r.exit === 0 && (!written(r) || r.outbox === '未见持续积压') &&
    (!r.resource_sampling || r.generator?.full_load === true);
  return r;
});

function lineChart(samples, field, title, unit, start, hold) {
  if (!samples.length) return '<p>没有足够的采样数据。</p>';
  if (!Number.isFinite(start) || start <= 0) return '<p>稳定施压未开始，无法绘制有效阶段曲线。</p>';
  const max = Math.max(1, ...samples.map(s => s[field]));
  const minT = samples[0].time, duration = Math.max(1, samples[samples.length - 1].time - minT);
  const x = t => Math.max(70, Math.min(800, 70 + (t - minT) / duration * 730));
  const pts = samples.map(s => `${70 + (s.time - minT) / duration * 730},${180 - s[field] / max * 145}`).join(' ');
  return `<figure><figcaption>${esc(title)}（${esc(unit)}）</figcaption><svg viewBox="0 0 840 220" role="img" aria-label="${esc(title)}">
    <rect x="${x(start)}" y="30" width="${x(start + hold) - x(start)}" height="150" fill="#e8effa"/>
    <path d="M70 30V180H810" fill="none" stroke="#a8b5c4"/><text x="8" y="40">${number(max, 1)}</text><text x="42" y="184">0</text>
    <path d="M${x(start)} 30V180M${x(start + hold)} 30V180" stroke="#52667d" stroke-dasharray="4 4"/><text x="${x(start)}" y="20">开始稳定施压</text><text x="${x(start + hold)}" y="20" text-anchor="end">停止施压</text>
    <polyline points="${pts}" fill="none" stroke="#c43738" stroke-width="2.5"/>
    <text x="70" y="207">${number(minT - start, 0)} 秒</text><text x="740" y="207">${number(minT + duration - start, 0)} 秒</text></svg></figure>`;
}
function bars(title, values, labels, unit) {
  const max = Math.max(1, ...values.filter(Number.isFinite));
  return `<figure><figcaption>${esc(title)}</figcaption>${values.map((v, i) => `<div class="bar-row"><span>${esc(labels[i])}</span>
    <div class="bar-track"><div class="bar" style="width:${Number.isFinite(v) ? v / max * 100 : 0}%;background:${labels[i].includes('未通过') ? '#c43738' : labels[i].startsWith('优化后') ? '#148078' : '#2459a9'}"></div></div>
    <strong>${number(v)} ${esc(unit)}</strong></div>`).join('')}</figure>`;
}
function hints(r) {
  const m = r.metrics, advice = [];
  if (!m.requests || m.p95 === null || r.exit !== 0 && r.exit !== 99) advice.push('运行未完整结束：先检查 k6.log 和 server.log，不能作为性能成绩。');
  if (m.business_failed > 0 || m.http_failed > 0) advice.push('先检查失败响应及服务日志，确认是否是请求、鉴权或服务错误。');
  if (m.p95 >= 500) advice.push(['signup', 'login'].includes(r.endpoint) ?
    '认证接口延迟超标：优先核对 bcrypt 的 CPU 开销，再检查数据库等待；目前只有接口测量证据。' :
    '接口延迟超标：优先检查 MySQL 连接池等待与查询/事务耗时，再决定是否改代码或索引。');
  if (m.dropped > 0) advice.push(`存在未发出的${r.endpoint === 'mixed' ? '流程' : '请求'}：检查 VU 是否足够、k6 与后端的同机资源竞争；本档不能当作后端吞吐上限。`);
  if (['持续积压', '重试异常'].includes(r.outbox)) advice.push('后台处理异常：先检查 Outbox Worker 与 API 共用连接池、投影处理及重试；HTTP 达标也不能算整条写链路通过。');
  if (r.outbox === '需确认') advice.push('后台样本不足或等待较长：看稳定阶段曲线，必要时延长测试；停压后归零不能证明负载可持续。');
  if (!advice.length) advice.push('本档已通过，暂未发现需要优化的证据。更高负载的容量需要另测。');
  return advice.map(a => `<li>${esc(a)}</li>`).join('');
}
const row = (r, prefix = '') => `<tr><td>${esc(names[r.endpoint])}</td><td>${esc(({ before: '优化前', after: '优化后', current: '当前' })[r.label] || r.label)}</td><td>${r.rate}</td><td>${number(r.success_rps)}</td>
  <td>${number(r.metrics.p95)}</td><td>${percent(r.metrics.http_failed)} / ${percent(r.metrics.business_failed)}</td>
  <td>${r.metrics.dropped}</td><td class="${r.pass ? 'ok' : 'bad'}">${r.endpoint === 'mixed' ? `${esc(r.generator.status)}；服务 HTTP ${r.service_http_pass ? '达标' : '未达标'}` : r.http_pass && r.exit === 0 ? '达标' : '未达标'} / ${esc(r.outbox)}</td>
  <td>${written(r) ? `${number(r.outbox_peak, 0)} / ${number(r.outbox_age, 3)}` : '不适用'}</td>
  <td><a href="${esc(prefix)}${esc(r.name)}/report.html">查看</a></td></tr>`;
const table = rows => `<div class="scroll"><table><thead><tr><th>接口</th><th>版本标签</th><th>目标 RPS</th><th>成功 RPS</th><th>p95 ms</th><th>HTTP / 业务失败</th><th>丢弃</th><th>HTTP / 后台</th><th>后台峰值 条 / 秒</th><th>详情</th></tr></thead><tbody>${rows}</tbody></table></div>`;

function mixedTables(r) {
  if (r.endpoint !== 'mixed') return '';
  if (!r.mixed) return '<p class="bad">缺少逐接口与流程数据，本轮无效。</p>';
  const endpoints = r.mixed.endpoints.map(e => `<tr><td>${esc(names[e.name])}</td><td>${number(e.expected_rps)}</td>
    <td>${number(e.requests / r.hold)}</td><td>${number(e.success_rps)}</td><td>${number(e.p95)}</td><td>${number(e.p99)}</td>
    <td>${percent(e.http_failed)} / ${percent(e.business_failed)}</td><td class="${e.pass ? 'ok' : 'bad'}">${e.pass ? '通过' : '未通过'}</td></tr>`).join('');
  const journeys = r.mixed.journeys.map(j => `<tr><td>${esc(journeyNames[j.name])}</td><td>${number(j.expected_per_second)}</td>
    <td>${j.vus}</td><td>${j.started ?? '无数据'} / ${j.completed ?? '无数据'}</td><td>${percent(j.completion_rate)}</td>
    <td>${percent(j.failed)}</td><td>${j.dropped}</td></tr>`).join('');
  const durations = r.mixed.journeys.flatMap(j => ['success', 'failure'].map(outcome => {
    const d = j.duration?.[outcome];
    return `<tr><td>${esc(journeyNames[j.name])}</td><td>${outcome === 'success' ? '成功' : '失败'}</td>
      <td>${d?.samples ?? '未采集'}</td><td>${number(d?.mean_ms)}</td><td>${number(d?.median_ms)}</td>
      <td>${number(d?.p95_ms)}</td><td>${number(d?.p99_ms)}</td><td>${percent(j.utilization)}</td></tr>`;
  })).join('');
  const resources = r.generator.resources;
  return `<h2>施压与服务分别判定</h2><p>施压：${esc(r.generator.status)}。服务 HTTP / 流程：${r.service_http_pass ? '通过' : '未通过'}；后台：${esc(r.outbox)}。施压不足时保留接口观测，但不能证明服务完整承载目标档位。两类问题可以同时存在。</p>
    ${resources ? `<p>稳定窗口 CPU 平均 / 峰值（核）：k6 ${number(resources.k6.mean_cpu)} / ${number(resources.k6.peak_cpu)}，服务 ${number(resources.server.mean_cpu)} / ${number(resources.server.peak_cpu)}，MySQL ${number(resources.mysql.mean_cpu)} / ${number(resources.mysql.peak_cpu)}；整机忙碌平均 ${percent(resources.host.mean_cpu)}。可用内存最低 ${number(resources.min_available_bytes / 1024 ** 2)} MiB。<a href="resources.csv">资源采样</a> · <a href="assessment.json">独立判定</a></p>` : '<p>历史样本未采集资源和流程耗时，保留原验收结果。</p>'}
    <h2>逐接口验收</h2><p>RATE 为完整执行时的预期 HTTP RPS。请求按开始时间划分稳定窗口；每个接口独立要求 p95 &lt; 500ms、HTTP / 业务失败率 &lt; 1%。p99 作为观测值。准备阶段不计入。</p>
    <div class="scroll"><table><thead><tr><th>接口</th><th>预期 RPS</th><th>实际 RPS</th><th>成功 RPS</th><th>p95 ms</th><th>p99 ms</th><th>HTTP / 业务失败</th><th>验收</th></tr></thead><tbody>${endpoints}</tbody></table></div>
    <h2>用户流程完成情况</h2><p>流程按开始时间分组，稳定阶段开始的流程可在 30 秒收尾内完成；每个请求最多等待 5 秒。前置失败会终止依赖请求，因此必须同时检查实际 RPS 与流程完成情况。丢弃单位为整个流程，不能伪称某接口丢弃请求数；丢弃包含升速阶段。</p>
    <div class="scroll"><table><thead><tr><th>流程</th><th>预期次数/秒</th><th>VU</th><th>开始 / 完成</th><th>完成率</th><th>失败率</th><th>丢弃流程</th></tr></thead><tbody>${journeys}</tbody></table></div>
    <p>整轮要求所有接口达标、全部已开始的稳定流程完成、无流程丢弃，并且后台未见持续积压。实际分配 ${r.mixed.allocated_vus} VU；${r.mixed.allocation_method === 'explicit' ? '本轮使用逐流程显式配额，运行期间固定；动态校准在测量轮次之间进行。' : `VUS 参数 ${r.vus} 按流程比例分配并向上取整，各流程至少 5 VU。`}</p>
    <h2>完整流程耗时</h2><p>按流程开始时间分组，包含请求、思考时间和收尾。成功与失败分别统计；零样本显示无数据。p99 仅供参考，少于 30 个成功样本时校准采用保守启动预算与观测最大值。占用率为所有跨越稳定窗口的流程在窗口内占用毫秒 / 窗口毫秒 / 配额；它是平均占用，不是峰值。</p>
    <div class="scroll"><table><thead><tr><th>流程</th><th>结果</th><th>样本数</th><th>均值 ms</th><th>中位数 ms</th><th>p95 ms</th><th>p99 ms（参考）</th><th>平均占用率</th></tr></thead><tbody>${durations}</tbody></table></div>
    <p>热点来自准备阶段热榜前两页的 20 篇帖子；浏览完列表后，80% 进入热门收藏/分享链接，20% 点击列表实际返回的帖子。发表评论流程同样以 80% 概率访问热点。列表 80% 访问前三页，其余访问第 4–20 页；请求间思考时间 0.2–0.8 秒。评论中 25% 尝试回复同帖评论。投票基于查询到的状态变更，覆盖赞成、反对和取消。</p>
    <p>评论回读只证明总数增长；并发追加时不证明新增 ID 位于第一页。发帖后详情读取来自 MySQL，列表采用异步投影，不要求新帖立即进入首屏；本轮不测逐事件投影可见延迟。比例与热点是假设，数据从基线随发帖、评论和注册增长。</p>
    <p><a href="endpoints.csv">下载逐接口 CSV</a> · <a href="journeys.csv">下载流程 CSV</a>。固定模型和实际热点选择计数见原始摘要。</p>`;
}

function csvRows(fields, rows) {
  const cell = value => `"${String(value ?? '').replace(/"/g, '""')}"`;
  return [fields.join(','), ...rows.map(row => row.map(cell).join(','))].join('\n') + '\n';
}

function endpointCSV(selected, includeRun = false) {
  const fields = ['endpoint', 'expected_rps', 'requests', 'success_rps', 'p95_ms', 'p99_ms', 'http_failure_rate', 'business_failure_rate', 'http_pass'];
  if (includeRun) fields.unshift('run', 'date_utc', 'hold_seconds', 'overall_pass', 'report');
  const rows = selected.flatMap(r => (r.mixed?.endpoints || []).map(e => {
    const values = [e.name, e.expected_rps, e.requests, e.success_rps, e.p95, e.p99, e.http_failed, e.business_failed, e.pass];
    return includeRun ? [r.name, r.date, r.hold, r.pass, `output/results/${r.name}/report.html`, ...values] : values;
  }));
  return csvRows(fields, rows);
}

function ledger() {
  const fields = ['run', 'date_utc', 'category', 'endpoint', 'label', 'target_rps', 'ramp_seconds', 'hold_seconds', 'vus',
    'requests', 'success_rps', 'p95_ms', 'p99_ms', 'http_failure_rate', 'business_failure_rate', 'dropped',
    'exit_code', 'result', 'outbox_status', 'outbox_pending_peak', 'outbox_oldest_peak_seconds',
    'commit', 'code_hash', 'script_hash', 'snapshot_hash', 'dataset', 'config', 'environment', 'thresholds', 'report',
    'generator_status', 'service_http_pass', 'mixed_allocation'];
  const cell = value => `"${String(value ?? '').replace(/"/g, '""')}"`;
  const rows = runs.map(r => {
    const category = r.hold < 30 ? '冒烟' : r.label === 'before' ? '优化前（待配对）' :
      r.label === 'after' ? '优化后（待配对）' : r.label !== 'current' ? '诊断' : r.hold < 180 ? '容量探测' : '稳定负载';
    const result = !r.valid ? '运行异常' : r.hold < 30 && r.http_pass && r.exit === 0 ? 'HTTP通过；后台容量未评估' : r.pass ? '通过' : '未通过';
    return [r.name, r.date, category, r.endpoint, r.label, r.rate, r.ramp, r.hold, r.vus,
      r.metrics.requests, r.success_rps, r.metrics.p95, r.metrics.p99, r.metrics.http_failed, r.metrics.business_failed,
      r.metrics.dropped, r.exit, result, r.outbox, r.outbox_peak, r.outbox_age, r.commit, r.code_hash,
      r.script_hash, r.snapshot_hash, JSON.stringify(r.dataset), JSON.stringify(r.config), JSON.stringify(r.environment),
      JSON.stringify(r.thresholds), `output/results/${r.name}/report.html`, r.generator?.status,
      r.service_http_pass ?? '', r.mixed?.allocation ? JSON.stringify(r.mixed.allocation) : ''].map(cell).join(',');
  });
  return [fields.join(','), ...rows].join('\n') + '\n';
}

function comparison() {
  if (__ENV.REPORT_MODE !== 'compare') return '';
  const before = runs.filter(r => r.label === 'before'), after = runs.filter(r => r.label === 'after');
  const reasons = [];
  if (runs.length !== 6 || before.length !== 3 || after.length !== 3) reasons.push('需要 before、after 各三轮。');
  if (runs.some(r => r.hold < 180)) reasons.push('正式对照每轮必须至少稳定施压 180 秒。');
  const same = fields => runs.every(r => fields.every(key => JSON.stringify(r[key]) === JSON.stringify(runs[0][key])));
  if (!same(['endpoint', 'rate', 'ramp', 'hold', 'vus', 'dataset', 'snapshot_hash', 'environment', 'script_hash', 'thresholds'])) reasons.push('接口、负载、时长、VU、环境、快照或脚本不一致。');
  if (runs.some(r => r.endpoint === 'mixed')) reasons.push('混合负载需逐接口与流程对照，不能用总体 p95 计算单接口优化收益。');
  if ([before, after].some(group => group.length && group.some(r => r.code_hash !== group[0].code_hash || JSON.stringify(r.config) !== JSON.stringify(group[0].config)))) reasons.push('同一版本的三轮代码或配置不一致。');
  if (runs.some(r => !r.snapshot_hash || !r.code_hash || !r.script_hash || !r.environment?.cpu)) reasons.push('缺少复现信息。');
  if (runs.some(r => r.metrics.p95 === null || r.success_rps === null || ![0, 99].includes(r.exit))) reasons.push('有轮次缺少测量数据或运行异常。');
  if (runs.some(r => written(r) && r.stable.length < 10)) reasons.push('缺少足够的稳定阶段后台样本。');
  let content = `<h2>优化前后对照</h2><p>每一根柱对应一轮；不挑选最好的一次。固定负载用于比较延迟，提高最大可用吞吐需要另外探测。</p>`;
  if (reasons.length) return content + `<p class="bad">无法给出有效提升比例：${esc(reasons.join(' '))}</p>`;
  const labels = runs.map(r => `${r.label === 'before' ? '优化前' : '优化后'} · ${r.name}`);
  content += bars('p95 响应时间（越低越好）', runs.map(r => r.metrics.p95), labels, 'ms') +
    bars('实际成功请求速率', runs.map(r => r.success_rps), labels, 'RPS');
  if (written(runs[0])) {
    content += bars('稳定阶段后台待处理峰值（越低越好）', runs.map(r => r.outbox_peak), labels, '条') +
      bars('稳定阶段最老事件等待峰值（越低越好）', runs.map(r => r.outbox_age), labels, '秒');
    const bAge = median(before.map(r => r.outbox_age)), aAge = median(after.map(r => r.outbox_age));
    content += `<p>三轮后台等待峰值的中位数：${number(bAge, 3)} → ${number(aAge, 3)} 秒；${aAge < bAge ? '降低' : '增加'} ${bAge > 0 ? percent(Math.abs(aAge - bAge) / bAge) : '无法计算'}。每两秒采样，可能漏掉短暂峰值。</p>`;
  }
  const b = median(before.map(r => r.metrics.p95)), a = median(after.map(r => r.metrics.p95));
  const configsChanged = JSON.stringify(before[0].config) !== JSON.stringify(after[0].config);
  content += `<p>三轮 p95 的中位数：${number(b)} → ${number(a)} ms；${a < b ? '降低' : '增加'} ${b > 0 ? percent(Math.abs(a - b) / b) : '无法计算'}。</p>`;
  content += `<p>优化前 ${before.filter(r => r.pass).length}/3 轮通过，优化后 ${after.filter(r => r.pass).length}/3 轮通过；${after.every(r => r.pass) ? '优化后三轮均通过 HTTP 与后台标准，验证了当前条件下此档负载；更高吞吐需要另测。' : '优化后仍有未通过轮次，不能宣称全部达标或可持续吞吐。'}</p>`;
  if (configsChanged) content += '<p>前后服务配置不同：这是一组配置调整对照，不能把收益全部归因于代码。</p>';
  if (before[0].code_hash === after[0].code_hash && !configsChanged) content += '<p class="bad">前后代码与配置相同：差异是测量波动，不能宣称优化收益。</p>';
  return content;
}
const css = `body{margin:0;background:#f4f7fb;color:#223449;font:16px/1.65 "Noto Sans CJK SC","Microsoft YaHei",sans-serif}main{max-width:1180px;margin:0 auto;padding:32px 24px 64px}h1{font-size:30px;margin:0 0 12px}h2{font-size:22px;margin:32px 0 12px}h3{font-size:18px}p,li{max-width:80ch}a{color:#2459a9}a:focus-visible{outline:3px solid #2459a9;outline-offset:3px}.lead{border-left:5px solid #2459a9;padding:12px 20px;background:#e8effa}.ok{color:#17695d}.bad{color:#b32c31;font-weight:600}.scroll{overflow-x:auto}table{border-collapse:collapse;width:100%;background:#fff;font-size:14px}th,td{padding:11px 12px;border-bottom:1px solid #d8e1eb;text-align:left;white-space:nowrap}th{background:#e8effa}figure{margin:20px 0;padding:18px;background:#fff}figcaption{font-weight:600;margin-bottom:14px}svg{display:block;width:100%;max-height:270px}svg text{font:13px sans-serif;fill:#52667d}.bar-row{display:grid;grid-template-columns:210px 1fr 130px;gap:14px;align-items:center;margin:12px 0;font-size:13px}.bar-track{height:20px;background:#edf1f6}.bar{height:100%}details{margin:18px 0;padding:14px;background:#fff}summary{cursor:pointer;font-weight:600}pre{overflow:auto;font-size:13px}small{color:#52667d}@media(max-width:680px){main{padding:24px 12px}.bar-row{grid-template-columns:1fr}.bar-track{height:16px}h1{font-size:25px}}`;
function page(title, content) {
  return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(title)}</title><style>${css}</style><main><h1>${esc(title)}</h1>${content}<p><small>本机测试；后端与 k6 共享硬件。结果不是生产容量承诺，单接口独立测量的容量不能相加。p95 表示约 95% 的请求在该时间内完成。</small></p></main></html>`;
}

export const options = { vus: 1, iterations: 1 };
export default function () {}
export function handleSummary() {
  const output = __ENV.REPORT_OUTPUT || 'tests/load/output/results/index.html';
  const files = {};
  if (__ENV.REPORT_MODE !== 'compare') for (const r of runs) {
    const verdict = r.pass ? '本档通过' : r.resource_sampling && !r.generator?.full_load ?
      `本档施压未通过（${r.generator.status}），服务结果另见逐接口验收` :
      r.endpoint === 'mixed' && r.service_http_pass ? '服务 HTTP 达标，后台仍需排查或确认' :
      r.http_pass && r.exit === 0 ? 'HTTP 达标，后台仍需排查或确认' : '本档未通过';
    let content = `<p class="lead ${r.pass ? 'ok' : 'bad'}">${verdict}：目标 ${r.rate} RPS，稳定 ${r.hold} 秒，实际成功 ${number(r.success_rps)} RPS，p95 ${number(r.metrics.p95)} ms。</p>`;
    content += table(row(r, '../')) + `<h2>下一步查哪里</h2><ul>${hints(r)}</ul>`;
    content += mixedTables(r);
    content += '<p>通过标准：p95 &lt; 500ms，HTTP 与业务失败率均 &lt; 1%，丢弃为 0。实际成功 RPS = 稳定阶段业务成功数 / 稳定秒数。</p>';
    content += '<h2>请求速率与响应时间曲线</h2><p><a href="charts.html">打开 k6 原生交互图表</a>。原生图表包含准备、升速和稳定阶段，正式数字以本页稳定阶段结果为准；极短测试可能没有图表。</p><iframe src="charts.html" title="k6 请求速率与响应时间图表" loading="lazy" style="width:100%;height:650px;border:1px solid #d8e1eb"></iframe>';
    if (written(r)) {
      content += `<h2>后台是否跟得上</h2><p>判断：${esc(r.outbox)}。采样每两秒一次，图中 0 秒是稳定施压开始；负数是准备/升速阶段，${r.hold} 秒之后是停压恢复。停压后归零不能替代稳定阶段检查。</p>`;
      content += lineChart(r.samples, 'pending', '待处理事件', '条', r.load_start_ms / 1000, r.hold) + lineChart(r.samples, 'oldest', '最老事件等待时间', '秒', r.load_start_ms / 1000, r.hold);
      content += '<p><small>自动筛查：稳定样本至少 10 个；末段与首段待处理数中位数增长 &gt; 10 且等待年龄增长 &gt; 1 秒，标记持续积压；等待始终 ≤ 1 秒且数量增长 ≤ 10，标记未见持续积压；其余需确认。它是保守筛查，不是精确消费容量。</small></p>';
    }
    content += `<details><summary>测试条件与原始结果</summary><p>每轮从相同快照恢复；初始 200 用户、5 社区、2000 帖子、20000 评论、20000 投票；发帖正文 4 KiB。${r.endpoint === 'mixed' ? '混合流程、热点与分页见上方说明。' : '读帖子随机取样，列表遍历前 20 页。'}准备阶段登录不计入正式数字。</p><pre>${esc(JSON.stringify({ date: r.date, endpoint: r.endpoint, rate: r.rate, ramp: r.ramp, hold: r.hold, vus: r.vus, environment: r.environment, dataset: r.dataset, config: r.config, commit: r.commit, code_hash: r.code_hash, script_hash: r.script_hash, snapshot_hash: r.snapshot_hash, thresholds: r.thresholds, mixed: r.mixed }, null, 2))}</pre><a href="summary.json">测量数值</a> · <a href="k6.log">k6 日志</a> · <a href="server.log">服务日志</a></details>`;
    if (r.endpoint === 'mixed' && r.mixed) {
      files[`${r.dir}/endpoints.csv`] = endpointCSV([r]);
      const fields = ['journey', 'expected_per_second', 'vus', 'started', 'completed', 'completion_rate', 'failed', 'dropped'];
      const durationFields = ['samples', 'mean_ms', 'median_ms', 'p95_ms', 'p99_ms', 'max_ms'];
      files[`${r.dir}/journeys.csv`] = csvRows([...fields, 'mean_busy_vus', 'utilization',
        ...['success', 'failure'].flatMap(o => durationFields.map(f => `${o}_${f}`))],
      r.mixed.journeys.map(j => [...fields.map(f => j[f === 'journey' ? 'name' : f]), j.mean_busy_vus, j.utilization,
        ...['success', 'failure'].flatMap(o => durationFields.map(f => j.duration?.[o]?.[f] ?? null))]));
      files[`${r.dir}/assessment.json`] = JSON.stringify({ generator: r.generator,
        service_http_pass: !!r.service_http_pass, service_pass: !!r.service_pass, outbox: r.outbox,
        valid: r.valid, overall_pass: r.pass }, null, 2);
      files[`${r.dir}/generator-verdict.txt`] = r.generator.full_load === true ? '0\n' : '1\n';
    }
    files[`${r.dir}/report.html`] = page(`${r.endpoint} · ${r.rate} RPS`, content);
    files[`${r.dir}/verdict.txt`] = r.pass ? '0\n' : '1\n';
  }
  let content = '<p class="lead">先找未达标接口，再看该接口的负载、延迟和后台积压曲线。绿色代表本档通过，红色代表未通过或需要确认。</p>' + comparison();
  content += '<h2>各轮实测</h2>' + table(runs.map(r => row(r)).join(''));
  if (__ENV.REPORT_MODE !== 'compare') {
    content += '<h2>已测负载区间</h2><p>下表只描述本页同环境、配置、代码和测试条件下的结果。短测通过档位是已测下界，首次失败档位仍需排查，不能直接当作精确极限。</p>';
    for (const endpoint of [...new Set(runs.map(r => r.endpoint))]) {
      const measured = runs.filter(r => r.endpoint === endpoint && r.hold >= 30 && r.valid &&
        ['current', 'before', 'after'].includes(r.label));
      const key = r => JSON.stringify([r.code_hash, r.script_hash, r.snapshot_hash, r.config, r.environment, r.hold, r.ramp, r.mixed?.model?.request_shares]);
      if (!measured.length) { content += `<p>${esc(names[endpoint])}：暂无正式容量样本，尚未测量负载区间。</p>`; continue; }
      const group = measured.filter(r => key(r) === key(measured[measured.length - 1]));
      const good = group.filter(r => r.pass).map(r => r.rate), bad = group.filter(r => !r.pass).map(r => r.rate);
      content += `<h3>${esc(names[endpoint])}（${esc(endpoint)}）</h3><p>最高已测通过 ${good.length ? Math.max(...good) + ' RPS' : '暂无'}；最低已测未通过 ${bad.length ? Math.min(...bad) + ' RPS' : '暂无，尚未找到失败档位'}。仅展示最新一组一致条件，${measured.length - group.length} 轮条件不同未合并。</p>`;
      content += bars('各档 p95 响应时间（通过标准 < 500ms）', group.map(r => r.metrics.p95), group.map(r => `${r.rate} RPS · ${r.pass ? '通过' : '未通过'}`), 'ms');
    }
  }
  files[output] = page(__ENV.REPORT_MODE === 'compare' ? 'Bluebell · 优化前后对照' : 'Bluebell · 压测报告', content);
  // 仅完整总览刷新仓库台账，局部批次或六轮对照不能覆盖全量记录。
  if (output.endsWith('/index.html') && __ENV.REPORT_MODE !== 'compare') {
    files['tests/load/records.csv'] = ledger();
    files['tests/load/mixed-records.csv'] = endpointCSV(runs.filter(r => r.endpoint === 'mixed'), true);
  }
  files.stdout = `HTML 报告：${output}\n`;
  return files;
}
