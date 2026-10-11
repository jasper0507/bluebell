// 原生 k6 离线校准：仅读取上一轮证据，生成下一轮配额和调整记录。
import { recommendAllocation, calibrationDecision } from './mixed-capacity.js';
const run = __ENV.CALIBRATION_FILE ? JSON.parse(open(__ENV.CALIBRATION_FILE)) : null;
const assessment = run ? JSON.parse(open(__ENV.CALIBRATION_FILE.replace(/summary\.json$/, 'assessment.json'))) : null;
const rate = Number(__ENV.CALIBRATION_RATE);
if (!Number.isInteger(rate) || rate <= 0 || rate % 20) throw new Error('CALIBRATION_RATE 必须为 20 的正整数倍');
const allocation = __ENV.CALIBRATION_KEEP === 'true' && run?.rate === rate ?
  run.mixed.allocation : recommendAllocation(run, rate, __ENV.CALIBRATION_ADJUST === 'true');
export const options = { vus: 1, iterations: 1 };
export default function () {}
export function handleSummary() {
  const output = __ENV.CALIBRATION_OUTPUT;
  return {
    [`${output}.json`]: JSON.stringify({ source: __ENV.CALIBRATION_FILE || null, rate,
      decision: run ? calibrationDecision(run, assessment.generator) : 'initial', allocation,
      generator: assessment?.generator || null,
      observations: run?.mixed?.journeys || [],
      rule: '均值 × 流程到达率 × 1.4 + 5；成功样本少于 30 时采用启动预算/观测最大值；丢弃或占用率 >75% 时至少增加 25%；同档丢弃还参考最长成功耗时 × 到达率 × 1.2 + 5；短测/长测反馈共享三次调整额度；p99 仅参考' }, null, 2),
    [`${output}.vus`]: JSON.stringify(allocation),
    [`${output}.decision`]: run ? calibrationDecision(run, assessment.generator) : 'initial',
    stdout: `校准记录：${output}.json\n`,
  };
}
