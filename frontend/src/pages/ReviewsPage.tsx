// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useCallback, useEffect, useState } from 'react';
import {
  listReviews,
  generateReview,
  type ReviewReport,
} from '@/api/reviews';
import { notifyError, notifySuccess } from '@/lib/notify';

function pct(v: number | null): string {
  if (v === null) return '—';
  return `${(v * 100).toFixed(1)}%`;
}

export default function ReviewsPage(): JSX.Element {
  const [items, setItems] = useState<ReviewReport[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<ReviewReport | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    listReviews()
      .then((r) => {
        setItems(r);
        setSelected((cur) => cur ?? r[0] ?? null);
      })
      .catch((e) => notifyError(e, '加载复盘失败'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const generate = async (kind: 'daily' | 'weekly') => {
    setBusy(true);
    try {
      const r = await generateReview(kind);
      notifySuccess(`已生成${kind === 'daily' ? '每日' : '每周'}复盘`);
      setSelected(r);
      load();
    } catch (e) {
      notifyError(e, '生成复盘失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">复盘 / Reviews</h1>
          <p className="mt-1 text-sm text-slate-500">
            自动汇总时段内推荐与 T+N 表现。每日 15:30 / 每周日自动生成，也可手动触发。
          </p>
        </div>
        <div className="flex gap-2">
          <button onClick={() => generate('daily')} disabled={busy} className="btn-secondary text-sm">
            生成每日
          </button>
          <button onClick={() => generate('weekly')} disabled={busy} className="btn-primary text-sm">
            生成每周
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[320px_1fr]">
        <ReviewList items={items} loading={loading} selectedId={selected?.id} onSelect={setSelected} />
        <ReviewDetail report={selected} />
      </div>
    </div>
  );
}

function ReviewList({
  items,
  loading,
  selectedId,
  onSelect,
}: {
  items: ReviewReport[];
  loading: boolean;
  selectedId?: number;
  onSelect: (r: ReviewReport) => void;
}): JSX.Element {
  if (loading) {
    return <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center text-sm text-slate-400">加载中…</div>;
  }
  if (items.length === 0) {
    return (
      <div className="rounded-2xl border border-dashed border-slate-200 bg-white p-6 text-center text-sm text-slate-400">
        暂无复盘。点右上角「生成」试试。
      </div>
    );
  }
  return (
    <div className="space-y-2">
      {items.map((r) => (
        <button
          key={r.id}
          onClick={() => onSelect(r)}
          className={`w-full rounded-xl border p-3 text-left transition-colors ${
            r.id === selectedId ? 'border-brand-400 bg-brand-50' : 'border-slate-200 bg-white hover:bg-slate-50'
          }`}
        >
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-slate-800">
              {r.kind === 'daily' ? '每日' : '每周'} · {r.period_start.slice(0, 10)}
            </span>
            <span className="text-[10px] uppercase text-slate-400">{r.llm}</span>
          </div>
          <div className="mt-1 text-xs text-slate-500">
            推荐 {r.recommendation_count} · T+5 胜率 {pct(r.win_rate_t5)}
          </div>
        </button>
      ))}
    </div>
  );
}

function ReviewDetail({ report }: { report: ReviewReport | null }): JSX.Element {
  if (!report) {
    return (
      <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-400">
        选择左侧一份复盘查看详情
      </div>
    );
  }
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-soft">
      <div className="flex flex-wrap gap-6 border-b border-slate-100 pb-4">
        <Metric label="类型" value={report.kind === 'daily' ? '每日' : '每周'} />
        <Metric label="时段" value={`${report.period_start.slice(0, 10)}`} />
        <Metric label="推荐数" value={String(report.recommendation_count)} />
        <Metric label="T+5 胜率" value={pct(report.win_rate_t5)} />
        <Metric label="T+5 平均收益" value={pct(report.avg_t5_return)} />
      </div>
      <pre className="mt-4 whitespace-pre-wrap font-sans text-sm leading-relaxed text-slate-700">
        {report.summary}
      </pre>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div>
      <div className="text-[11px] text-slate-400">{label}</div>
      <div className="mt-0.5 text-sm font-semibold text-slate-900">{value}</div>
    </div>
  );
}
