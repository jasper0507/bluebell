// 原生 k6 离线报告：不发 HTTP 请求，不安装报告库。
const paths = (__ENV.REPORT_FILES || '').split(',').filter(Boolean);
if (!paths.length) throw new Error('REPORT_FILES 必须包含 summary.json 的绝对路径，以逗号分隔');
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const number = (v, digits = 2) => Number.isFinite(v) ? v.toFixed(digits) : '无数据';
const percent = v => Number.isFinite(v) ? `${number(v * 100)}%` : '无数据';
const median = a => { const s = [...a].sort((x, y) => x - y); return s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2; };
const written = r => ['create_post', 'vote'].includes(r.endpoint);
const names = { signup: '注册', login: '登录', post: '帖子详情', posts_time: '时间列表', create_post: '发帖', vote: '投票' };
const runs = paths.map(path => {
  const r = JSON.parse(open(path));
  if (r.schema !== 1) throw new Error(`不兼容的历史报告：${path}`);
  r.dir = path.slice(0, path.lastIndexOf('/'));
  r.name = r.dir.slice(r.dir.lastIndexOf('/') + 1);
  r.exit = Number(open(`${r.dir}/exit-code.txt`).trim());
  r.success_rps = r.metrics.successes === null ? null : r.metrics.successes / r.hold;
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
  r.pass = r.http_pass && r.exit === 0 && (!written(r) || r.outbox === '未见持续积压');
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
  if (m.dropped > 0) advice.push('存在未发出的请求：检查 VU 是否足够、k6 与后端的同机资源竞争；本档不能当作后端吞吐上限。');
  if (['持续积压', '重试异常'].includes(r.outbox)) advice.push('后台处理异常：先检查 Outbox Worker 与 API 共用连接池、投影处理及重试；HTTP 达标也不能算整条写链路通过。');
  if (r.outbox === '需确认') advice.push('后台样本不足或等待较长：看稳定阶段曲线，必要时延长测试；停压后归零不能证明负载可持续。');
  if (!advice.length) advice.push('本档已通过，暂未发现需要优化的证据。更高负载的容量需要另测。');
  return advice.map(a => `<li>${esc(a)}</li>`).join('');
}
const row = (r, prefix = '') => `<tr><td>${esc(names[r.endpoint])}</td><td>${esc(({ before: '优化前', after: '优化后', current: '当前' })[r.label] || r.label)}</td><td>${r.rate}</td><td>${number(r.success_rps)}</td>
  <td>${number(r.metrics.p95)}</td><td>${percent(r.metrics.http_failed)} / ${percent(r.metrics.business_failed)}</td>
  <td>${r.metrics.dropped}</td><td class="${r.pass ? 'ok' : 'bad'}">${r.http_pass && r.exit === 0 ? '达标' : '未达标'} / ${esc(r.outbox)}</td>
  <td>${written(r) ? `${number(r.outbox_peak, 0)} / ${number(r.outbox_age, 3)}` : '不适用'}</td>
  <td><a href="${esc(prefix)}${esc(r.name)}/report.html">查看</a></td></tr>`;
const table = rows => `<div class="scroll"><table><thead><tr><th>接口</th><th>版本标签</th><th>目标 RPS</th><th>成功 RPS</th><th>p95 ms</th><th>HTTP / 业务失败</th><th>丢弃</th><th>HTTP / 后台</th><th>后台峰值 条 / 秒</th><th>详情</th></tr></thead><tbody>${rows}</tbody></table></div>`;

function ledger() {
  const fields = ['run', 'date_utc', 'category', 'endpoint', 'label', 'target_rps', 'ramp_seconds', 'hold_seconds', 'vus',
    'requests', 'success_rps', 'p95_ms', 'p99_ms', 'http_failure_rate', 'business_failure_rate', 'dropped',
    'exit_code', 'result', 'outbox_status', 'outbox_pending_peak', 'outbox_oldest_peak_seconds',
    'commit', 'code_hash', 'script_hash', 'snapshot_hash', 'dataset', 'config', 'environment', 'thresholds', 'report'];
  const cell = value => `"${String(value ?? '').replace(/"/g, '""')}"`;
  const rows = runs.map(r => {
    const category = r.hold < 30 ? '冒烟' : r.label === 'before' ? '优化前（待配对）' :
      r.label === 'after' ? '优化后（待配对）' : r.label !== 'current' ? '诊断' : r.hold < 180 ? '容量探测' : '稳定负载';
    const result = !r.valid ? '运行异常' : r.hold < 30 && r.http_pass && r.exit === 0 ? 'HTTP通过；后台容量未评估' : r.pass ? '通过' : '未通过';
    return [r.name, r.date, category, r.endpoint, r.label, r.rate, r.ramp, r.hold, r.vus,
      r.metrics.requests, r.success_rps, r.metrics.p95, r.metrics.p99, r.metrics.http_failed, r.metrics.business_failed,
      r.metrics.dropped, r.exit, result, r.outbox, r.outbox_peak, r.outbox_age, r.commit, r.code_hash,
      r.script_hash, r.snapshot_hash, JSON.stringify(r.dataset), JSON.stringify(r.config), JSON.stringify(r.environment),
      JSON.stringify(r.thresholds), `output/results/${r.name}/report.html`].map(cell).join(',');
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
  content += `<p>优化后 ${after.filter(r => r.pass).length}/3 轮通过；${runs.every(r => r.pass) ? '前后均达标，可在注明条件后引用延迟变化。' : '存在未通过轮次，不能宣称全部达标或可持续吞吐。'}</p>`;
  if (configsChanged) content += '<p>前后服务配置不同：这是一组配置调整对照，不能把收益全部归因于代码。</p>';
  if (before[0].code_hash === after[0].code_hash && !configsChanged) content += '<p class="bad">前后代码与配置相同：差异是测量波动，不能宣称优化收益。</p>';
  return content;
}
const css = `body{margin:0;background:#f4f7fb;color:#223449;font:16px/1.65 "Noto Sans CJK SC","Microsoft YaHei",sans-serif}main{max-width:1180px;margin:0 auto;padding:32px 24px 64px}h1{font-size:30px;margin:0 0 12px}h2{font-size:22px;margin:32px 0 12px}h3{font-size:18px}p,li{max-width:80ch}a{color:#2459a9}a:focus-visible{outline:3px solid #2459a9;outline-offset:3px}.lead{border-left:5px solid #2459a9;padding:12px 20px;background:#e8effa}.ok{color:#17695d}.bad{color:#b32c31;font-weight:600}.scroll{overflow-x:auto}table{border-collapse:collapse;width:100%;background:#fff;font-size:14px}th,td{padding:11px 12px;border-bottom:1px solid #d8e1eb;text-align:left;white-space:nowrap}th{background:#e8effa}figure{margin:20px 0;padding:18px;background:#fff}figcaption{font-weight:600;margin-bottom:14px}svg{display:block;width:100%;max-height:270px}svg text{font:13px sans-serif;fill:#52667d}.bar-row{display:grid;grid-template-columns:210px 1fr 130px;gap:14px;align-items:center;margin:12px 0;font-size:13px}.bar-track{height:20px;background:#edf1f6}.bar{height:100%}details{margin:18px 0;padding:14px;background:#fff}summary{cursor:pointer;font-weight:600}pre{overflow:auto;font-size:13px}small{color:#52667d}@media(max-width:680px){main{padding:24px 12px}.bar-row{grid-template-columns:1fr}.bar-track{height:16px}h1{font-size:25px}}`;
function page(title, content) {
  return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(title)}</title><style>${css}</style><main><h1>${esc(title)}</h1>${content}<p><small>本机单接口测试；后端与 k6 共享硬件。结果不是生产容量承诺，各接口 RPS 不能相加。p95 表示约 95% 的请求在该时间内完成。</small></p></main></html>`;
}

export const options = { vus: 1, iterations: 1 };
export default function () {}
export function handleSummary() {
  const output = __ENV.REPORT_OUTPUT || 'tests/load/output/results/index.html';
  const files = {};
  if (__ENV.REPORT_MODE !== 'compare') for (const r of runs) {
    let content = `<p class="lead ${r.pass ? 'ok' : 'bad'}">${r.pass ? '本档通过' : r.http_pass && r.exit === 0 ? 'HTTP 达标，后台仍需排查或确认' : '本档未通过'}：目标 ${r.rate} RPS，稳定 ${r.hold} 秒，实际成功 ${number(r.success_rps)} RPS，p95 ${number(r.metrics.p95)} ms。</p>`;
    content += table(row(r, '../')) + `<h2>下一步查哪里</h2><ul>${hints(r)}</ul>`;
    content += '<p>通过标准：p95 &lt; 500ms，HTTP 与业务失败率均 &lt; 1%，丢弃为 0。实际成功 RPS = 稳定阶段业务成功数 / 稳定秒数。</p>';
    content += '<h2>请求速率与响应时间曲线</h2><p><a href="charts.html">打开 k6 原生交互图表</a>。原生图表包含准备、升速和稳定阶段，正式数字以本页稳定阶段结果为准；极短测试可能没有图表。</p><iframe src="charts.html" title="k6 请求速率与响应时间图表" loading="lazy" style="width:100%;height:650px;border:1px solid #d8e1eb"></iframe>';
    if (written(r)) {
      content += `<h2>后台是否跟得上</h2><p>判断：${esc(r.outbox)}。采样每两秒一次，图中 0 秒是稳定施压开始；负数是准备/升速阶段，${r.hold} 秒之后是停压恢复。停压后归零不能替代稳定阶段检查。</p>`;
      content += lineChart(r.samples, 'pending', '待处理事件', '条', r.load_start_ms / 1000, r.hold) + lineChart(r.samples, 'oldest', '最老事件等待时间', '秒', r.load_start_ms / 1000, r.hold);
      content += '<p><small>自动筛查：稳定样本至少 10 个；末段与首段待处理数中位数增长 &gt; 10 且等待年龄增长 &gt; 1 秒，标记持续积压；等待始终 ≤ 1 秒且数量增长 ≤ 10，标记未见持续积压；其余需确认。它是保守筛查，不是精确消费容量。</small></p>';
    }
    content += `<details><summary>测试条件与原始结果</summary><p>每轮从相同快照恢复；200 用户、5 社区、2000 帖子、20000 评论、20000 投票；发帖正文 4 KiB；读帖子随机取样，列表遍历前 20 页。准备阶段登录不计入正式数字。</p><pre>${esc(JSON.stringify({ date: r.date, endpoint: r.endpoint, rate: r.rate, ramp: r.ramp, hold: r.hold, vus: r.vus, environment: r.environment, dataset: r.dataset, config: r.config, commit: r.commit, code_hash: r.code_hash, script_hash: r.script_hash, snapshot_hash: r.snapshot_hash, thresholds: r.thresholds }, null, 2))}</pre><a href="summary.json">测量数值</a> · <a href="k6.log">k6 日志</a> · <a href="server.log">服务日志</a></details>`;
    files[`${r.dir}/report.html`] = page(`${r.endpoint} · ${r.rate} RPS`, content);
    files[`${r.dir}/verdict.txt`] = r.pass ? '0\n' : '1\n';
  }
  let content = '<p class="lead">先找未达标接口，再看该接口的负载、延迟和后台积压曲线。绿色代表本档通过，红色代表未通过或需要确认。</p>' + comparison();
  content += '<h2>各轮实测</h2>' + table(runs.map(r => row(r)).join(''));
  if (__ENV.REPORT_MODE !== 'compare') {
    content += '<h2>已测负载区间</h2><p>下表只描述本页同环境、配置、代码和测试条件下的结果。短测通过档位是已测下界，首次失败档位仍需排查，不能直接当作精确极限。</p>';
    for (const endpoint of [...new Set(runs.map(r => r.endpoint))]) {
      const measured = runs.filter(r => r.endpoint === endpoint && r.hold >= 30 && r.valid);
      const key = r => JSON.stringify([r.code_hash, r.script_hash, r.snapshot_hash, r.config, r.environment, r.hold, r.ramp]);
      if (!measured.length) { content += `<p>${esc(names[endpoint])}：只有冒烟结果，尚未测量负载区间。</p>`; continue; }
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
  }
  files.stdout = `HTML 报告：${output}\n`;
  return files;
}
