// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import ReactECharts from 'echarts-for-react';
import {
  getDashboard,
  getAggregations,
  type DashboardSummary,
  type AggregationRow,
} from '@/api/dashboard';
import { listPositions, type PositionSummary } from '@/api/positions';

function pct(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—';
  return `${(v * 100).toFixed(1)}%`;
}

export default function Dashboard(): JSX.Element {
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [aggs, setAggs] = useState<AggregationRow[]>([]);
  const [posSummary, setPosSummary] = useState<PositionSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    // Stats are the core; win-rate aggregations and positions are
    // best-effort (the perf engine may be disabled or have no T+5 data
    // yet), so they must not break the whole page.
    getDashboard(7)
      .then((s) => alive && setSummary(s))
      .catch((e) => alive && setError(e?.message ?? 'load failed'))
      .finally(() => alive && setLoading(false));
    getAggregations('strategy')
      .then((a) => alive && setAggs(a.items ?? []))
      .catch(() => alive && setAggs([]));
    listPositions()
      .then((p) => alive && setPosSummary(p.summary))
      .catch(() => alive && setPosSummary(null));
    return () => {
      alive = false;
    };
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">仪表盘 / Dashboard</h1>
        <p className="mt-1 text-sm text-slate-500">
          近 {summary?.runs_window_days ?? 7} 天运行概览 · 数据实时来自选股引擎
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          加载失败：{error}
        </div>
      )}

      <StatCards summary={summary} loading={loading} />
      <PositionStrip summary={posSummary} />
      <WinRateChart rows={aggs} loading={loading} />
      <ReturnHeatmap rows={aggs} />
      <RecentErrors summary={summary} />
    </div>
  );
}

function StatCards({
  summary,
  loading,
}: {
  summary: DashboardSummary | null;
  loading: boolean;
}): JSX.Element {
  const cards = [
    { title: '今日新增推荐', value: summary ? String(summary.today_recommendations) : '—', hint: 'Today' },
    { title: '累计推荐', value: summary ? String(summary.total_recommendations) : '—', hint: 'Total' },
    {
      title: '运行成功率',
      value: summary ? pct(summary.success_rate) : '—',
      hint: `${summary?.runs_success ?? 0}/${summary?.runs_total ?? 0}`,
    },
    { title: '失败运行', value: summary ? String(summary.runs_failed) : '—', hint: 'Failed' },
  ];
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {cards.map((c) => (
        <div key={c.title} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-slate-700">{c.title}</span>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] uppercase tracking-wider text-slate-400">
              {c.hint}
            </span>
          </div>
          <div className="mt-4 text-3xl font-semibold text-slate-900">
            {loading ? <span className="text-slate-300">···</span> : c.value}
          </div>
        </div>
      ))}
    </div>
  );
}

function WinRateChart({ rows, loading }: { rows: AggregationRow[]; loading: boolean }): JSX.Element {
  const ranked = [...rows]
    .filter((r) => r.win_rate_t5 !== null)
    .sort((a, b) => (b.win_rate_t5 ?? 0) - (a.win_rate_t5 ?? 0))
    .slice(0, 10);

  const option = {
    grid: { left: 8, right: 24, top: 16, bottom: 8, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (p: { name: string; value: number }[]) =>
        `${p[0].name}<br/>T+5 胜率: ${p[0].value}%`,
    },
    xAxis: { type: 'value', max: 100, axisLabel: { formatter: '{value}%' } },
    yAxis: { type: 'category', data: ranked.map((r) => r.key), inverse: true },
    series: [
      {
        type: 'bar',
        data: ranked.map((r) => Number(((r.win_rate_t5 ?? 0) * 100).toFixed(1))),
        itemStyle: { color: '#4f46e5', borderRadius: [0, 4, 4, 0] },
        barWidth: '60%',
      },
    ],
  };

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
      <h2 className="text-sm font-semibold text-slate-700">方案胜率排行（T+5）</h2>
      {loading ? (
        <div className="mt-6 flex h-64 items-center justify-center text-sm text-slate-400">加载中…</div>
      ) : ranked.length === 0 ? (
        <div className="mt-6 flex h-64 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-slate-200 text-sm text-slate-400">
          <span>暂无胜率数据</span>
          <span className="text-xs">运行方案并等推荐达到 T+5 后这里会显示排行</span>
        </div>
      ) : (
        <ReactECharts option={option} style={{ height: 64 + ranked.length * 32 }} notMerge />
      )}
    </div>
  );
}

function RecentErrors({ summary }: { summary: DashboardSummary | null }): JSX.Element {
  const errors = summary?.recent_errors ?? [];
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
      <h2 className="text-sm font-semibold text-slate-700">最近运行错误</h2>
      {errors.length === 0 ? (
        <p className="mt-3 text-sm text-slate-400">近期无失败运行 🎉</p>
      ) : (
        <ul className="mt-3 divide-y divide-slate-100">
          {errors.map((e) => (
            <li key={e.run_id} className="flex items-center justify-between gap-4 py-2 text-sm">
              <Link to={`/runs/${e.run_id}`} className="font-medium text-brand-600 hover:underline">
                运行 #{e.run_id}
              </Link>
              <span className="flex-1 truncate text-slate-500" title={e.error}>
                {e.error || '(无错误信息)'}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function PositionStrip({ summary }: { summary: PositionSummary | null }): JSX.Element {
  const pnl = summary?.total_pnl ?? null;
  const pnlCls = pnl === null ? 'text-slate-400' : pnl > 0 ? 'text-red-600' : pnl < 0 ? 'text-green-600' : 'text-slate-600';
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-slate-700">当前持仓</h2>
        <Link to="/positions" className="text-xs text-brand-600 hover:underline">
          查看持仓 →
        </Link>
      </div>
      {!summary || summary.count === 0 ? (
        <p className="mt-3 text-sm text-slate-400">暂无持仓。在「推荐」页点「标记建仓」加入。</p>
      ) : (
        <div className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Metric label="持仓数" value={String(summary.count)} />
          <Metric label="总成本" value={summary.total_cost_value.toLocaleString('zh-CN', { maximumFractionDigits: 0 })} />
          <Metric
            label="总市值"
            value={summary.total_market_value === null ? '—' : summary.total_market_value.toLocaleString('zh-CN', { maximumFractionDigits: 0 })}
          />
          <Metric
            label="总盈亏"
            value={pnl === null ? '—' : pnl.toLocaleString('zh-CN', { maximumFractionDigits: 0 })}
            cls={pnlCls}
          />
        </div>
      )}
    </div>
  );
}

function Metric({ label, value, cls }: { label: string; value: string; cls?: string }): JSX.Element {
  return (
    <div>
      <div className="text-xs text-slate-400">{label}</div>
      <div className={`mt-1 text-xl font-semibold ${cls ?? 'text-slate-900'}`}>{value}</div>
    </div>
  );
}

function ReturnHeatmap({ rows }: { rows: AggregationRow[] }): JSX.Element {
  // strategy (rows) × {T+1, T+3, T+5} avg return (%). Built from the
  // same aggregations payload — no extra request.
  const horizons: { label: string; key: keyof AggregationRow }[] = [
    { label: 'T+1', key: 'avg_t1_return' },
    { label: 'T+3', key: 'avg_t3_return' },
    { label: 'T+5', key: 'avg_t5_return' },
  ];
  const usable = rows.filter((r) =>
    horizons.some((h) => r[h.key] !== null && r[h.key] !== undefined),
  );

  if (usable.length === 0) {
    return (
      <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
        <h2 className="text-sm font-semibold text-slate-700">收益热力图（方案 × T+N 平均收益）</h2>
        <div className="mt-6 flex h-40 flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-slate-200 text-sm text-slate-400">
          <span>暂无 T+N 收益数据</span>
          <span className="text-xs">推荐达到对应 T+N 交易日后这里会显示</span>
        </div>
      </div>
    );
  }

  // ECharts heatmap data: [xIndex, yIndex, value%]
  const data: [number, number, number][] = [];
  usable.forEach((r, y) => {
    horizons.forEach((h, x) => {
      const v = r[h.key] as number | null;
      if (v !== null && v !== undefined) {
        data.push([x, y, Number((v * 100).toFixed(2))]);
      }
    });
  });
  const maxAbs = Math.max(1, ...data.map((d) => Math.abs(d[2])));

  const option = {
    grid: { left: 8, right: 24, top: 16, bottom: 24, containLabel: true },
    tooltip: {
      position: 'top',
      formatter: (p: { data: [number, number, number]; marker: string }) =>
        `${usable[p.data[1]].key}<br/>${horizons[p.data[0]].label} 平均收益: ${p.data[2]}%`,
    },
    xAxis: { type: 'category', data: horizons.map((h) => h.label), splitArea: { show: true } },
    yAxis: { type: 'category', data: usable.map((r) => r.key), splitArea: { show: true } },
    visualMap: {
      min: -maxAbs,
      max: maxAbs,
      calculable: true,
      orient: 'horizontal',
      left: 'center',
      bottom: 0,
      // green (loss) → white → red (gain), A-share convention.
      inRange: { color: ['#16a34a', '#f1f5f9', '#dc2626'] },
    },
    series: [
      {
        type: 'heatmap',
        data,
        label: { show: true, formatter: (p: { data: [number, number, number] }) => `${p.data[2]}%` },
        emphasis: { itemStyle: { shadowBlur: 8, shadowColor: 'rgba(0,0,0,0.2)' } },
      },
    ],
  };

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-soft">
      <h2 className="text-sm font-semibold text-slate-700">收益热力图（方案 × T+N 平均收益）</h2>
      <ReactECharts option={option} style={{ height: 120 + usable.length * 36 }} notMerge />
    </div>
  );
}
