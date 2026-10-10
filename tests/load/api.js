import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Counter, Gauge, Rate } from 'k6/metrics';

const endpoint = __ENV.ENDPOINT || 'posts_time';
const rate = Number(__ENV.RATE || 10);
const ramp = Number(__ENV.RAMP_SECONDS || 10);
const hold = Number(__ENV.HOLD_SECONDS || 60);
const vus = Number(__ENV.VUS || Math.max(10, Math.ceil(rate * 0.75) + 5));
const base = 'http://127.0.0.1:18080/api/v1';
const fixture = JSON.parse(open(__ENV.DATA_FILE || './output/state/data.json'));
if (!['signup', 'login', 'post', 'posts_time', 'create_post', 'vote'].includes(endpoint)) throw new Error(`未知 ENDPOINT: ${endpoint}`);
for (const [name, value] of Object.entries({ RATE: rate, HOLD_SECONDS: hold, VUS: vus })) {
  if (!Number.isInteger(value) || value <= 0) throw new Error(`${name} 必须是正整数`);
}
if (!Number.isInteger(ramp) || ramp < 0) throw new Error('RAMP_SECONDS 必须是非负整数');
if (['create_post', 'vote'].includes(endpoint) && ramp + hold + 30 >= fixture.config.access_token_ttl_seconds) {
  throw new Error('本轮超过访问令牌有效期，请缩短时长');
}
const succeeded = new Counter('business_success');
const failed = new Rate('business_failed');
const loadStart = new Gauge('load_start_ms');
let started = false;
const runID = __ENV.RUN_ID || String(Date.now());

export const options = {
  setupTimeout: '90s',
  scenarios: { api: {
    executor: 'ramping-arrival-rate', startRate: ramp ? 1 : rate, timeUnit: '1s', preAllocatedVUs: vus,
    stages: [...(ramp ? [{ target: rate, duration: `${ramp}s` }] : []), { target: rate, duration: `${hold}s` }],
    gracefulStop: '5s',
  } },
  summaryTrendStats: ['med', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    'http_reqs{phase:load}': ['count>0'],
    'http_req_duration{phase:load}': ['p(95)<500'],
    'http_req_failed{phase:load}': ['rate<0.01'],
    'business_failed{phase:load}': ['rate<0.01'],
    'business_success{phase:load}': ['count>=0'],
    dropped_iterations: ['count==0'],
  },
};

function json(response) { try { return response.json(); } catch (_) { return null; } }

export function setup() {
  const params = { tags: { phase: 'setup', name: 'fixture_check' }, timeout: '5s' };
  const detail = http.get(`${base}/posts/${fixture.posts[0]}`, params);
  const list = http.get(`${base}/posts?order=time`, params);
  if (detail.status !== 200 || json(detail)?.code !== 'OK' || list.status !== 200 ||
      json(list)?.code !== 'OK' || !json(list)?.data?.items?.length) throw new Error('测试数据或 Redis 排行榜未就绪');
  const tokens = [];
  if (['create_post', 'vote'].includes(endpoint)) {
    for (const username of fixture.users) {
      const r = http.post(`${base}/login`, JSON.stringify({ username, password: fixture.password }), {
        ...params, headers: { 'Content-Type': 'application/json' }, tags: { phase: 'setup', name: 'setup_login' },
      });
      const body = json(r);
      if (r.status !== 200 || body?.code !== 'OK' || !body?.data?.access_token) throw new Error(`测试账号登录失败：${username}`);
      tokens.push(body.data.access_token);
    }
  }
  return { tokens };
}

export default function ({ tokens }) {
  if (!started) { loadStart.add(exec.scenario.startTime + ramp * 1000); started = true; }
  const iteration = exec.scenario.iterationInTest;
  const actor = (exec.vu.idInTest - 1) % fixture.users.length;
  const post = fixture.posts[Math.floor(Math.random() * fixture.posts.length)];
  const phase = Date.now() - exec.scenario.startTime < ramp * 1000 ? 'warmup' : 'load';
  const params = { headers: { 'Content-Type': 'application/json' }, tags: { phase, name: endpoint }, timeout: '5s' };
  if (tokens.length) params.headers.Authorization = `Bearer ${tokens[actor]}`;
  let method = 'GET', path, payload = null, status = 200;
  switch (endpoint) {
    case 'signup':
      method = 'POST'; path = '/signup'; status = 201;
      payload = { username: `lt${runID}${iteration}`, password: fixture.password, confirm_password: fixture.password }; break;
    case 'login':
      method = 'POST'; path = '/login'; payload = { username: fixture.users[actor], password: fixture.password }; break;
    case 'post': path = `/posts/${post}`; break;
    case 'posts_time': path = `/posts?order=time&page=${1 + iteration % 20}`; break;
    case 'create_post':
      method = 'POST'; path = '/posts'; status = 201;
      payload = { title: `loadtest ${runID} ${iteration}`, content: 'x'.repeat(4096),
        community_id: fixture.communities[iteration % fixture.communities.length] }; break;
    case 'vote': method = 'PUT'; path = `/posts/${post}/vote`; payload = { direction: [-1, 0, 1][iteration % 3] }; break;
  }
  const response = http.request(method, `${base}${path}`, payload ? JSON.stringify(payload) : null, params);
  const body = json(response);
  const ok = check(response, { '状态、业务结果和数据正确': r => r.status === status && body?.code === 'OK' &&
    (endpoint !== 'posts_time' || body?.data?.items?.length > 0) }, { phase });
  failed.add(!ok, { phase });
  succeeded.add(ok ? 1 : 0, { phase });
}

export function handleSummary(data) {
  const value = (name, stat) => data.metrics[name]?.values?.[stat] ?? null;
  const result = {
    schema: 1, id: runID, date: new Date().toISOString(), endpoint, rate, ramp, hold, vus,
    load_start_ms: value('load_start_ms', 'value'),
    dataset: fixture.dataset, config: fixture.config,
    environment: { kernel: __ENV.LOAD_KERNEL, cpu: __ENV.LOAD_CPU, cores: __ENV.LOAD_CORES,
      memory_kib: __ENV.LOAD_MEMORY_KIB, go: __ENV.LOAD_GO, k6: __ENV.LOAD_K6, mysql: __ENV.LOAD_MYSQL, redis: __ENV.LOAD_REDIS },
    commit: __ENV.LOAD_COMMIT, code_hash: __ENV.LOAD_CODE_HASH, script_hash: __ENV.LOAD_SCRIPT_HASH,
    snapshot_hash: __ENV.LOAD_SNAPSHOT_HASH, label: __ENV.LABEL || 'current',
    metrics: {
      requests: value('http_reqs{phase:load}', 'count'), successes: value('business_success{phase:load}', 'count'),
      p95: value('http_req_duration{phase:load}', 'p(95)'), p99: value('http_req_duration{phase:load}', 'p(99)'),
      http_failed: value('http_req_failed{phase:load}', 'rate'), business_failed: value('business_failed{phase:load}', 'rate'),
      dropped: value('dropped_iterations', 'count') ?? 0,
    },
    http_pass: Object.values(data.metrics).every(m => Object.values(m.thresholds || {}).every(t => t.ok)),
    thresholds: { p95_ms: 500, failure_rate: 0.01, dropped: 0 },
  };
  const m = result.metrics;
  result.http_pass = result.http_pass && m.requests > 0 && m.p95 !== null && m.business_failed !== null;
  return {
    [`${__ENV.RESULT_DIR || '.'}/summary.json`]: JSON.stringify(result, null, 2),
    stdout: `\n${endpoint}：${result.http_pass ? 'HTTP 达标' : 'HTTP 未达标'}\n目标 ${rate} RPS；实际成功 ${((m.successes || 0) / hold).toFixed(2)} RPS；p95 ${m.p95?.toFixed(2) ?? '无数据'} ms\nHTTP 失败 ${((m.http_failed || 0) * 100).toFixed(2)}%；业务失败 ${((m.business_failed || 0) * 100).toFixed(2)}%；丢弃 ${m.dropped}\n写接口还需检查 HTML 中的后台积压曲线。\n`,
  };
}
