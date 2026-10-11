// 业务流程的到达率独立于响应速度；RATE 是完整流程下的预期 HTTP RPS。
import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

const base = 'http://127.0.0.1:18080/api/v1';
const started = new Counter('journey_started');
const completed = new Counter('journey_completed');
const journeyFailed = new Rate('journey_failed');
const failures = new Counter('failure_samples');
const selections = new Counter('post_selection');
const duration = new Trend('journey_duration', true);
const durationSamples = new Counter('journey_duration_samples');
const busy = new Counter('journey_busy_ms');
const workflows = { guest: 180, member: 60, commenter: 20, publisher: 10, returning: 30, newcomer: 5 };
const shares = { posts_time: 0.19, posts_hot: 0.06, post: 0.27, comments: 0.28,
  get_vote: 0.06, vote: 0.06, create_comment: 0.02, create_post: 0.01, login: 0.045, signup: 0.005 };
let failureLogs = 0;

export function mixedAllocation(vus) {
  if (!__ENV.MIXED_VUS) return Object.fromEntries(Object.entries(workflows)
    .map(([name, weight]) => [name, Math.max(5, Math.ceil(vus * weight / 305))]));
  const allocation = JSON.parse(__ENV.MIXED_VUS);
  if (!allocation || Array.isArray(allocation) || Object.keys(allocation).length !== 6 ||
      Object.keys(allocation).some(name => !(name in workflows)) ||
      Object.keys(workflows).some(name => !Number.isInteger(allocation[name]) || allocation[name] < 1)) {
    throw new Error('MIXED_VUS 必须是六个流程名到正整数 VU 的完整 JSON 映射');
  }
  return allocation;
}

export function mixedOptions(rate, ramp, hold, vus) {
  // 10 秒 timeUnit 保留 100/500 RPS 等比例下的半次/秒注册流程。
  if (rate % 20) throw new Error('混合负载 RATE 必须是 20 的正整数倍');
  const scenarios = {};
  const thresholds = {};
  const allocation = mixedAllocation(vus);
  for (const [name, perSecond] of Object.entries(workflows)) {
    const target = perSecond * rate / 100;
    scenarios[name] = { executor: 'ramping-arrival-rate', timeUnit: '10s', startRate: ramp ? 0 : target,
      preAllocatedVUs: allocation[name],
      stages: [...(ramp ? [{ target, duration: `${ramp}s` }] : []), { target, duration: `${hold}s` }],
      gracefulStop: '30s' };
    thresholds[`journey_started{phase:load,journey:${name}}`] = ['count>0'];
    thresholds[`journey_completed{phase:load,journey:${name}}`] = ['count>0'];
    thresholds[`journey_failed{phase:load,journey:${name}}`] = ['rate<0.01'];
    thresholds[`dropped_iterations{scenario:${name}}`] = ['count==0'];
    thresholds[`journey_duration{phase:load,journey:${name}}`] = ['avg>=0'];
    thresholds[`journey_busy_ms{phase:load,journey:${name}}`] = ['count>=0'];
    for (const outcome of ['success', 'failure']) {
      thresholds[`journey_duration{phase:load,journey:${name},outcome:${outcome}}`] = ['avg>=0'];
      thresholds[`journey_duration_samples{phase:load,journey:${name},outcome:${outcome}}`] = ['count>=0'];
    }
  }
  for (const name of Object.keys(shares)) {
    const tag = `{phase:load,name:${name}}`;
    thresholds[`http_reqs${tag}`] = ['count>0'];
    thresholds[`http_req_duration${tag}`] = ['p(95)<500'];
    thresholds[`http_req_failed${tag}`] = ['rate<0.01'];
    thresholds[`business_failed${tag}`] = ['rate<0.01'];
    thresholds[`business_success${tag}`] = ['count>=0'];
  }
  thresholds['post_selection{phase:load,source:hotspot}'] = ['count>=0'];
  thresholds['post_selection{phase:load,source:list}'] = ['count>=0'];
  return { scenarios, thresholds };
}

const parse = r => { try { return r.json(); } catch (_) { return null; } };
const pick = items => items[Math.floor(Math.random() * items.length)];
const pause = () => sleep(0.2 + Math.random() * 0.6);
const listValid = d => Number.isInteger(d?.total) && d.total >= 0 && Array.isArray(d.items) &&
  d.items.every(i => Number.isInteger(i.id) && i.id > 0 && typeof i.author_username === 'string');
const detailValid = (d, id) => d?.id === id && typeof d.content === 'string' &&
  Number.isInteger(d.up_votes) && Number.isInteger(d.down_votes) && d.up_votes >= 0 && d.down_votes >= 0;

export function mixedSetup(fixture) {
  const hot = http.get(`${base}/posts?order=hot&page=1`, { timeout: '5s', tags: { phase: 'setup', name: 'hotspot_check' } });
  const second = http.get(`${base}/posts?order=hot&page=2`, { timeout: '5s', tags: { phase: 'setup', name: 'hotspot_check' } });
  const items = [parse(hot), parse(second)];
  if ([hot, second].some(r => r.status !== 200) || items.some(b => b?.code !== 'OK' || !listValid(b.data))) {
    throw new Error('热榜基线检查失败');
  }
  const hotspots = items.flatMap(b => b.data.items.map(i => i.id));
  if (hotspots.length !== 20 || new Set(hotspots).size !== 20) throw new Error('热榜不足 20 篇不同帖子');
  const comments = http.get(`${base}/posts/${hotspots[0]}/comments`, {
    timeout: '5s', tags: { phase: 'setup', name: 'comment_check' },
  });
  if (comments.status !== 200 || parse(comments)?.code !== 'OK' || !listValid(parse(comments)?.data)) {
    throw new Error('评论基线检查失败');
  }
  return { hotspots };
}

export function runMixed(data, fixture, ramp, hold, runID, succeeded, failed) {
  const journey = exec.scenario.name;
  const iteration = exec.scenario.iterationInTest;
  const actor = (exec.vu.idInTest - 1) % fixture.users.length;
  const phase = () => {
    const elapsed = Date.now() - exec.scenario.startTime;
    return elapsed < ramp * 1000 ? 'warmup' : elapsed < (ramp + hold) * 1000 ? 'load' : 'cooldown';
  };
  const cohort = { phase: phase(), journey };
  const began = Date.now();
  started.add(1, cohort);
  let ok = false;
  function request(name, method, path, payload, token, validate, status = 200) {
    const tags = { phase: phase(), name, journey };
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers.Authorization = `Bearer ${token}`;
    const r = http.request(method, `${base}${path}`, payload ? JSON.stringify(payload) : null,
      { headers, tags, timeout: '5s' });
    const body = parse(r);
    const pass = check(r, { '状态、业务结果和响应数据正确': () =>
      r.status === status && body?.code === 'OK' && validate(body.data) }, tags);
    failed.add(!pass, tags);
    succeeded.add(pass ? 1 : 0, tags);
    if (!pass) {
      failures.add(1, { ...tags, status: String(r.status), code: String(body?.code || 'NO_JSON') });
      // 每个 VU 最多两条诊断；不打印响应正文、密码或令牌。
      if (failureLogs++ < 2) console.warn(JSON.stringify({ journey, endpoint: name, phase: tags.phase,
        status: r.status, code: body?.code || 'NO_JSON', error_code: r.error_code || 0 }));
    }
    return pass ? body.data : null;
  }
  const token = data.tokens[actor];
  const login = username => request('login', 'POST', '/login', { username, password: fixture.password }, null,
    d => d?.username === username && typeof d.access_token === 'string' && d.access_token.length > 0);
  const detail = id => request('post', 'GET', `/posts/${id}`, null, null, d => detailValid(d, id));
  const comments = (id, page = 1, validate = listValid) => request('comments', 'GET', `/posts/${id}/comments?page=${page}`,
    null, null, d => listValid(d) && d.page === page && d.page_size === 10 && validate(d));
  function browse() {
    const order = iteration % 4 === 3 ? 'hot' : 'time';
    const page = Math.random() < 0.8 ? 1 + iteration % 3 : 4 + iteration % 17;
    const list = request(`posts_${order}`, 'GET', `/posts?order=${order}&page=${page}`, null, null,
      d => listValid(d) && d.items.length > 0 && d.page === page && d.page_size === 10);
    if (!list) return null;
    // 先浏览列表，再进入热门收藏/分享链接；其余请求点击实际返回的帖子。
    const hotspot = Math.random() < 0.8;
    const id = hotspot ? pick(data.hotspots) : pick(list.items).id;
    selections.add(1, { phase: phase(), source: hotspot ? 'hotspot' : 'list' });
    pause();
    if (!detail(id)) return null;
    pause();
    return comments(id) ? id : null;
  }
  function execute() {
    switch (journey) {
      case 'guest': return browse() !== null;
      case 'member': {
        const id = browse();
        if (id === null) return false;
        pause();
        const previous = request('get_vote', 'GET', `/posts/${id}/vote`, null, token, d => [-1, 0, 1].includes(d?.direction));
        if (!previous) return false;
        pause();
        const direction = previous.direction === 0 ? (Math.random() < 0.9 ? 1 : -1) :
          (Math.random() < 0.8 ? 0 : -previous.direction);
        return !!request('vote', 'PUT', `/posts/${id}/vote`, { direction }, token,
          d => d?.post_id === id && d.direction === direction);
      }
      case 'commenter': {
        const id = Math.random() < 0.8 ? pick(data.hotspots) : pick(fixture.posts);
        if (!detail(id)) return false;
        pause();
        const before = comments(id);
        if (!before) return false;
        pause();
        const payload = { content: `mixed ${runID} ${iteration} ` + '讨论内容。'.repeat(40) };
        if (before.items.length && iteration % 4 === 0) payload.reply_to_comment_id = pick(before.items).id;
        const created = request('create_comment', 'POST', `/posts/${id}/comments`, payload, token,
          d => Number.isInteger(d?.id) && d.id > 0, 201);
        if (!created) return false;
        pause();
        // 评论按升序分页；并发追加时只验总数增长，不假定新评论位于第一页。
        return !!comments(id, 1, d => d.total >= before.total + 1);
      }
      case 'publisher': {
        const auth = login(fixture.users[actor]);
        if (!auth) return false;
        pause();
        const created = request('create_post', 'POST', '/posts', {
          title: `mixed ${runID} ${iteration}`, content: 'x'.repeat(4096),
          community_id: fixture.communities[iteration % fixture.communities.length],
        }, auth.access_token, d => Number.isInteger(d?.id) && d.id > 0, 201);
        if (!created) return false;
        pause();
        if (!detail(created.id)) return false;
        pause();
        // 排行榜是异步投影；不把新帖必须立即出现在首屏作为业务成功条件。
        return !!request('posts_time', 'GET', '/posts?order=time&page=1', null, null,
          d => listValid(d) && d.items.length > 0 && d.page === 1);
      }
      case 'returning': return !!login(fixture.users[actor]);
      case 'newcomer': {
        const username = `mx${runID}${iteration}`;
        if (!request('signup', 'POST', '/signup', { username, password: fixture.password,
          confirm_password: fixture.password }, null, d => d?.username === username, 201)) return false;
        pause();
        return !!login(username);
      }
      default: throw new Error(`未知业务流程 ${journey}`);
    }
  }
  try {
    ok = execute();
  } finally {
    const ended = Date.now();
    const tags = { ...cohort, outcome: ok ? 'success' : 'failure' };
    duration.add(ended - began, tags);
    durationSamples.add(1, tags);
    // 仅计算稳定窗口内的槽位占用，包含跨入/跨出的流程；不把收尾时间算入占用率。
    const windowStart = exec.scenario.startTime + ramp * 1000;
    const overlap = Math.max(0, Math.min(ended, windowStart + hold * 1000) - Math.max(began, windowStart));
    busy.add(overlap, { phase: 'load', journey });
    journeyFailed.add(!ok, cohort);
    completed.add(ok ? 1 : 0, cohort);
  }
}

export function mixedSummary(data, rate, hold, vus) {
  const value = (metric, stat) => data.metrics[metric]?.values?.[stat] ?? null;
  const endpoints = Object.entries(shares).map(([name, share]) => {
    const tag = `{phase:load,name:${name}}`;
    const m = { name, expected_rps: rate * share, requests: value(`http_reqs${tag}`, 'count'),
      successes: value(`business_success${tag}`, 'count'), p95: value(`http_req_duration${tag}`, 'p(95)'),
      p99: value(`http_req_duration${tag}`, 'p(99)'), http_failed: value(`http_req_failed${tag}`, 'rate'),
      business_failed: value(`business_failed${tag}`, 'rate') };
    m.success_rps = m.successes === null ? null : m.successes / hold;
    m.pass = m.requests > 0 && Number.isFinite(m.p95) && m.p95 < 500 &&
      Number.isFinite(m.http_failed) && m.http_failed < 0.01 && Number.isFinite(m.business_failed) && m.business_failed < 0.01;
    return m;
  });
  const journeys = Object.entries(workflows).map(([name, perSecond]) => {
    const tag = `{phase:load,journey:${name}}`;
    const count = value(`journey_started${tag}`, 'count');
    const done = value(`journey_completed${tag}`, 'count');
    const dropped = value(`dropped_iterations{scenario:${name}}`, 'count') ?? 0;
    const stats = outcome => {
      const tags = `{phase:load,journey:${name},outcome:${outcome}}`;
      const samples = value(`journey_duration_samples${tags}`, 'count') ?? 0;
      const measured = stat => samples > 0 ? value(`journey_duration${tags}`, stat) : null;
      return { samples, mean_ms: measured('avg'), median_ms: measured('med'),
        p95_ms: measured('p(95)'), p99_ms: measured('p(99)'), max_ms: measured('max') };
    };
    const slots = mixedAllocation(vus)[name];
    const busyMS = value(`journey_busy_ms${tag}`, 'count');
    return { name, expected_per_second: perSecond * rate / 1000,
      vus: slots, started: count, completed: done,
      duration: { success: stats('success'), failure: stats('failure') },
      mean_busy_vus: busyMS === null ? null : busyMS / (hold * 1000),
      utilization: busyMS === null ? null : busyMS / (hold * 1000 * slots),
      completion_rate: count > 0 ? done / count : null, failed: value(`journey_failed${tag}`, 'rate'), dropped };
  });
  return { endpoints, journeys, allocation: mixedAllocation(vus),
    allocation_method: __ENV.MIXED_VUS ? 'explicit' : 'legacy-proportional',
    model: { request_shares: shares, workflow_rates_at_1000: workflows,
    hotspot_source: '准备阶段热榜前两页，固定整轮使用', hotspot_probability: 0.8, hotspot_count: 20, first_three_pages_probability: 0.8,
    think_seconds: [0.2, 0.8], reply_probability: 0.25, request_window: '请求开始时间落在稳定窗口内；流程按开始时间分组，包含收尾结果',
    comment_readback: '仅校验总数增长，不证明新增 ID 出现在当前页',
    actual_hotspot_selections: value('post_selection{phase:load,source:hotspot}', 'count'),
    actual_list_selections: value('post_selection{phase:load,source:list}', 'count') },
    allocated_vus: journeys.reduce((sum, j) => sum + j.vus, 0) };
}
