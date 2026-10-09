import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';

// 每次只选一个接口；每次迭代只发一个目标请求。
export const endpoints = [
  'signup', 'login', 'refresh', 'logout',
  'communities', 'community',
  'posts_time', 'posts_hot', 'posts_community_time', 'posts_community_hot',
  'post', 'create_post', 'delete_post', 'get_vote', 'vote',
  'comments', 'create_comment', 'reply', 'delete_comment',
];
const endpoint = __ENV.ENDPOINT || 'posts_time';
const base = __ENV.BASE_URL || 'http://127.0.0.1:18080';
const rate = Number(__ENV.RATE || 10);
const ramp = Number(__ENV.RAMP_SECONDS || 10);
const hold = Number(__ENV.HOLD_SECONDS || 60);
const vus = Number(__ENV.VUS || Math.max(10, Math.ceil(rate * 0.75) + 5));
if (!endpoints.includes(endpoint)) throw new Error(`未知 ENDPOINT: ${endpoint}`);
if (base !== 'http://127.0.0.1:18080') throw new Error('本脚本只允许本机压测端口 18080');
for (const [name, value] of Object.entries({ RATE: rate, HOLD_SECONDS: hold, VUS: vus })) {
  if (!Number.isInteger(value) || value <= 0) throw new Error(`${name} 必须是正整数`);
}
if (!Number.isInteger(ramp) || ramp < 0) throw new Error('RAMP_SECONDS 必须是非负整数');

const dataFile = __ENV.DATA_FILE || './output/state/data.json';
const shared = (field) => new SharedArray(field, () => JSON.parse(open(dataFile))[field]);
const users = shared('users');
const posts = shared('posts');
const targets = shared('targets');
const sessions = shared('sessions');
const meta = shared('meta')[0];
if (meta.endpoint !== endpoint || meta.pool < rate * (ramp + hold)) {
  throw new Error('数据准备参数不匹配，请通过 make loadtest 运行');
}
if (Date.now() + (ramp + hold + 5) * 1000 >= meta.expires_at * 1000) {
  throw new Error('访问令牌有效期不足以覆盖本轮，请缩短时长或调整压测配置');
}

export const options = {
  scenarios: {
    api: {
      executor: 'ramping-arrival-rate',
      startRate: ramp ? 1 : rate,
      timeUnit: '1s',
      preAllocatedVUs: vus,
      stages: [
        ...(ramp ? [{ target: rate, duration: `${ramp}s` }] : []),
        { target: rate, duration: `${hold}s` },
      ],
      gracefulStop: '5s',
    },
  },
  noCookiesReset: true,
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    'http_reqs{phase:load}': ['count>0'],
    'http_req_duration{phase:load}': ['p(95)<500'],
    'http_req_failed{phase:load}': ['rate<0.01'],
    'checks{phase:load}': ['rate>0.99'],
    dropped_iterations: ['count==0'],
  },
};

function body(response) {
  try { return response.json(); } catch (_) { return null; }
}

export function setup() {
  const params = { tags: { phase: 'setup', name: 'fixture_check' }, timeout: '5s' };
  const detail = http.get(`${base}/api/v1/posts/${posts[0].id}`, params);
  const list = http.get(`${base}/api/v1/posts?order=time`, params);
  if (detail.status !== 200 || body(detail)?.code !== 'OK' ||
      list.status !== 200 || body(list)?.code !== 'OK' || !body(list)?.data?.items?.length) {
    throw new Error('压测数据或 Redis 排行榜未就绪');
  }
}

let refreshInitialized = false;
export default function () {
  const iteration = exec.scenario.iterationInTest;
  const actor = (exec.vu.idInTest - 1) % users.length;
  const user = users[actor];
  const post = posts[Math.floor(Math.random() * posts.length)];
  const community = meta.communities[Math.floor(Math.random() * meta.communities.length)];
  const phase = Date.now() - exec.scenario.startTime < ramp * 1000 ? 'warmup' : 'load';
  const params = {
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}` },
    tags: { phase, name: endpoint },
    timeout: '5s',
  };
  let method = 'GET';
  let path;
  let payload = null;
  let status = 200;
  let list = false;
  switch (endpoint) {
    case 'signup':
      method = 'POST'; path = '/signup'; status = 201;
      payload = { username: `lt${meta.run}${iteration}`, password: meta.password, confirm_password: meta.password };
      break;
    case 'login':
      method = 'POST'; path = '/login';
      payload = { username: user.username, password: meta.password };
      break;
    case 'refresh':
      if (!refreshInitialized) {
        http.cookieJar().set(base, 'refresh_token', sessions[exec.vu.idInTest - 1]);
        refreshInitialized = true;
      }
      method = 'POST'; path = '/refresh';
      break;
    case 'logout':
      if (!sessions[iteration]) exec.test.abort('一次性会话已耗尽，请重新准备数据');
      http.cookieJar().set(base, 'refresh_token', sessions[iteration]);
      method = 'POST'; path = '/logout'; status = 204;
      break;
    case 'communities': path = '/communities'; break;
    case 'community': path = `/communities/${community}`; break;
    case 'posts_time': case 'posts_hot':
      path = `/posts?order=${endpoint === 'posts_hot' ? 'hot' : 'time'}&page=${1 + iteration % 20}`;
      list = true; break;
    case 'posts_community_time': case 'posts_community_hot':
      path = `/posts?order=${endpoint.endsWith('hot') ? 'hot' : 'time'}&community_id=${community}&page=${1 + iteration % 10}`;
      list = true; break;
    case 'post': path = `/posts/${post.id}`; break;
    case 'create_post':
      method = 'POST'; path = '/posts'; status = 201;
      payload = { title: `loadtest ${meta.run} ${iteration}`, content: 'x'.repeat(4096), community_id: community };
      break;
    case 'delete_post': case 'delete_comment': {
      const target = targets[iteration];
      if (!target) exec.test.abort('一次性删除资源已耗尽，请重新准备数据');
      params.headers.Authorization = `Bearer ${users[target.user].token}`;
      method = 'DELETE'; status = 204;
      path = endpoint === 'delete_post' ? `/posts/${target.id}` : `/comments/${target.id}`;
      break;
    }
    case 'get_vote': path = `/posts/${post.id}/vote`; break;
    case 'vote':
      method = 'PUT'; path = `/posts/${post.id}/vote`;
      payload = { direction: [-1, 0, 1][iteration % 3] }; break;
    case 'comments':
      // 前 1000 篇复用的种子帖均有 20 条评论，不压空页。
      path = `/posts/${posts[iteration % 1000].id}/comments?page=${1 + iteration % 2}`;
      list = true; break;
    case 'create_comment': case 'reply':
      method = 'POST'; path = `/posts/${posts[iteration % 1000].id}/comments`; status = 201;
      payload = { content: 'loadtest comment '.repeat(16) };
      if (endpoint === 'reply') payload.reply_to_comment_id = posts[iteration % 1000].comment;
      break;
  }
  const response = http.request(method, `${base}/api/v1${path}`, payload ? JSON.stringify(payload) : null, params);
  const result = status === 204 ? null : body(response);
  check(response, {
    'HTTP 状态和业务响应正确': (r) => r.status === status &&
      (status === 204 || result?.code === 'OK') &&
      (!list || result?.data?.items?.length > 0),
  }, { phase });
}
