// 校准与报告共用的判定；不发请求，不改变测量原件。
export const workflowRates = { guest: 180, member: 60, commenter: 20, publisher: 10, returning: 30, newcomer: 5 };
const steps = { guest: 3, member: 5, commenter: 4, publisher: 4, returning: 1, newcomer: 2 };

export function recommendAllocation(previous, rate, adjust = false) {
  return Object.fromEntries(Object.entries(workflowRates).map(([name, weight]) => {
    const journey = previous?.mixed?.journeys?.find(j => j.name === name);
    const success = journey?.duration?.success;
    // 少于 30 个成功样本时取观测最大值与保守启动预算的较大值；p99 不参与配额计算。
    const bootstrap = (steps[name] - 1) * 0.8 + steps[name] * 0.5;
    const seconds = success?.samples >= 30 && Number.isFinite(success.mean_ms) ?
      success.mean_ms / 1000 : Math.max(bootstrap, (success?.max_ms || 0) / 1000);
    let slots = Math.max(5, Math.ceil(weight * rate / 1000 * seconds * 1.4) + 5);
    if (adjust && journey) {
      slots = Math.max(slots, journey.vus);
      if (journey.dropped > 0 || journey.utilization > 0.75 ||
          journey.started < Math.floor(journey.expected_per_second * previous.hold) - 1) {
        slots = Math.max(slots, Math.ceil(journey.vus * 1.25));
      }
      // 正式轮的实际丢弃说明均值预算不足；仅在同档反馈中吸收已观测的尾部，随后重测。
      if (journey.dropped > 0 && previous.rate === rate && Number.isFinite(success?.max_ms)) {
        slots = Math.max(slots, Math.ceil(weight * rate / 1000 * success.max_ms / 1000 * 1.2) + 5);
      }
    }
    return [name, slots];
  }));
}

export function generatorAssessment(run, csv) {
  if (!run.resource_sampling) return { status: '历史样本未校准', full_load: null, resources: null };
  const start = run.load_start_ms / 1000;
  const samples = csv.trim().split('\n').slice(1).map(line => {
    const [time, source, cpu, rss, available, total] = line.split(',');
    return { time: Number(time), source, cpu: Number(cpu), rss: Number(rss),
      available: Number(available), total: Number(total) };
  }).filter(s => s.time >= start && s.time < start + run.hold &&
    [s.time, s.cpu, s.rss, s.available, s.total].every(Number.isFinite) && s.total > 0);
  const bySource = name => samples.filter(s => s.source === name);
  const resource = name => {
    const rows = bySource(name);
    return { samples: rows.length, mean_cpu: rows.length ? rows.reduce((n, s) => n + s.cpu, 0) / rows.length : null,
      peak_cpu: rows.length ? Math.max(...rows.map(s => s.cpu)) : null,
      peak_rss_bytes: rows.length ? Math.max(...rows.map(s => s.rss)) : null };
  };
  const resources = { server: resource('server'), k6: resource('k6'), mysql: resource('mysql'), host: resource('host'),
    min_available_bytes: samples.length ? Math.min(...samples.map(s => s.available)) : null };
  if (['server', 'k6', 'mysql', 'host'].some(name => resources[name].samples < 3)) {
    return { status: '资源采样不足', full_load: false, resources };
  }
  const k6Rows = bySource('k6'), cores = Number(run.environment.cores);
  const lowMemory = k6Rows.filter(s => s.available < Math.max(256 * 1024 ** 2, s.total * 0.1)).length >= 3;
  const k6Saturated = !Number.isFinite(cores) || cores <= 0 ||
    k6Rows.filter(s => s.cpu >= cores * 0.8).length >= 3;
  const missingStarts = run.mixed?.journeys?.some(j =>
    !Number.isFinite(j.started) || j.started < Math.floor(j.expected_per_second * run.hold) - 1);
  const full = run.mixed?.journeys?.length === 6 && run.metrics.dropped === 0 && !missingStarts;
  return { status: lowMemory || k6Saturated ? '施压器资源不足' : full ? '完整施压' : '施压未校准',
    full_load: !!full && !lowMemory && !k6Saturated, resources,
    low_memory: lowMemory, k6_cpu_saturated: k6Saturated,
    missing_starts: !!missingStarts };
}

export function calibrationDecision(run, assessment) {
  if (['施压器资源不足', '资源采样不足'].includes(assessment.status)) return 'stop';
  return assessment.full_load && run.mixed.journeys.every(j =>
    Number.isFinite(j.utilization) && j.utilization <= 0.75) ? 'ready' : 'adjust';
}
