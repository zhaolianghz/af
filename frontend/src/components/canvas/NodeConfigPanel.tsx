// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * NodeConfigPanel — the right rail. Renders a type-specific form
 * for the currently selected node. Changes go through the canvas
 * store, which marks the graph as dirty.
 */
import { useMemo, useState } from 'react';
import { useCanvasStore } from '@/stores/canvasStore';
import type { NodeType, RFNode } from '@/types/orchestrator';
import {
  NodeTypeFields,
  isDataSourceSubtype,
  isIndicatorSubtype,
} from './nodeForms';
import useConfirm from '@/hooks/useConfirm';

export default function NodeConfigPanel(): JSX.Element {
  const selectedId = useCanvasStore((s) => s.selectedNodeId);
  const nodes = useCanvasStore((s) => s.nodes);
  const updateNodeData = useCanvasStore((s) => s.updateNodeData);
  const removeNode = useCanvasStore((s) => s.removeNode);
  const confirm = useConfirm();

  const node = useMemo(
    () => nodes.find((n) => n.id === selectedId) ?? null,
    [nodes, selectedId],
  );

  const onDelete = async () => {
    if (!node) return;
    const ok = await confirm({ title: '删除节点', message: node.id, danger: true });
    if (ok) removeNode(node.id);
  };

  const panel = node ? (
    <aside className="flex h-full w-80 flex-col border-l border-slate-200 bg-white">
      <div className="border-b border-slate-200 px-4 py-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-900">节点配置</h2>
          <button
            type="button"
            onClick={onDelete}
            className="rounded-md px-2 py-1 text-[11px] font-medium text-rose-600 hover:bg-rose-50"
          >
            删除
          </button>
        </div>
        <p className="mt-0.5 font-mono text-[11px] text-slate-500">{node.id}</p>
      </div>

      <div className="flex-1 space-y-4 overflow-auto p-4">
        <NodeForm node={node} onUpdate={(patch) => updateNodeData(node.id, patch)} />
      </div>
    </aside>
  ) : (
    <aside className="flex h-full w-80 flex-col border-l border-slate-200 bg-white">
      <div className="flex h-full items-center justify-center p-6 text-center text-xs text-slate-400">
        <div>
          <p>未选中节点</p>
          <p className="mt-1">点击画布中的节点以编辑参数</p>
        </div>
      </div>
    </aside>
  );

  return (
    <>
      {confirm.dialog}
      {panel}
    </>
  );
}

// =============================================================================
// NodeForm — type-dispatched
// =============================================================================

function NodeForm({
  node,
  onUpdate,
}: {
  node: RFNode;
  onUpdate: (patch: Partial<RFNode['data']>) => void;
}): JSX.Element {
  switch (node.type as NodeType) {
    case 'data_source':
      return <DataSourceForm node={node} onUpdate={onUpdate} />;
    case 'indicator':
      return <IndicatorForm node={node} onUpdate={onUpdate} />;
    case 'filter':
      return <FilterForm node={node} onUpdate={onUpdate} />;
    case 'rank':
      return <RankForm node={node} onUpdate={onUpdate} />;
    case 'dedupe':
      return <DedupeForm node={node} onUpdate={onUpdate} />;
    case 'session_tag':
      return <EmptyForm note="session_tag 节点无配置项" />;
    case 'persist':
      return <PersistForm node={node} onUpdate={onUpdate} />;
    case 'notify':
      return <NotifyForm node={node} onUpdate={onUpdate} />;
  }
}

// =============================================================================
// Type-specific forms
// =============================================================================

function DataSourceForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const subtype = (node.data.subtype ?? 'kline') as string;
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  const stockCodes = Array.isArray(params.stock_codes)
    ? (params.stock_codes as string[]).join(',')
    : '';

  return (
    <>
      <Field label="数据类型">
        <select
          className="form-select"
          value={subtype}
          onChange={(e) => onUpdate({ subtype: e.target.value })}
        >
          {NodeTypeFields.dataSource.subtypes.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </Field>

      {isDataSourceSubtype(subtype) && (
        <>
          <Field label="股票池 (Universe)">
            <select
              className="form-select"
              value={(params.universe as string) ?? ''}
              onChange={(e) => onUpdate({ params: { ...params, universe: e.target.value } })}
            >
              <option value="">不使用(仅用下方代码)</option>
              <option value="hs300">沪深 300</option>
              <option value="sse50">上证 50</option>
              <option value="zz500">中证 500</option>
              <option value="zz1000">中证 1000</option>
              <option value="all">全部 A 股</option>
            </select>
          </Field>
          {(params.universe as string) ? (
            <Field label="股票池上限 (每只单独拉取,建议 ≤ 50)">
              <input
                type="number"
                className="form-input"
                value={(params.universe_limit as number) ?? 20}
                min={1}
                max={500}
                onChange={(e) =>
                  onUpdate({ params: { ...params, universe_limit: Number(e.target.value) } })
                }
              />
            </Field>
          ) : null}
          <Field label="股票代码 (逗号分隔,与股票池合并)">
            <input
              type="text"
              className="form-input"
              value={stockCodes}
              onChange={(e) =>
                onUpdate({
                  params: {
                    ...params,
                    stock_codes: e.target.value
                      .split(',')
                      .map((s) => s.trim())
                      .filter(Boolean),
                  },
                })
              }
              placeholder="600519.SH, 000858.SZ"
            />
          </Field>
          {subtype === 'kline' && (
            <Field label="周期">
              <select
                className="form-select"
                value={(params.period as string) ?? '1d'}
                onChange={(e) => onUpdate({ params: { ...params, period: e.target.value } })}
              >
                <option value="1d">日 K</option>
                <option value="1h">60 分 K</option>
                <option value="30m">30 分 K</option>
                <option value="15m">15 分 K</option>
                <option value="5m">5 分 K</option>
              </select>
            </Field>
          )}
          <Field label="回溯天数">
            <input
              type="number"
              className="form-input"
              value={(params.days as number) ?? 60}
              min={1}
              max={500}
              onChange={(e) =>
                onUpdate({ params: { ...params, days: Number(e.target.value) } })
              }
            />
          </Field>
        </>
      )}
    </>
  );
}

function IndicatorForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const subtype = (node.data.subtype ?? 'ma') as string;
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  return (
    <>
      <Field label="指标类型">
        <select
          className="form-select"
          value={subtype}
          onChange={(e) => onUpdate({ subtype: e.target.value })}
        >
          {NodeTypeFields.indicator.subtypes.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </Field>
      {isIndicatorSubtype(subtype) && (
        <Field label="周期">
          <input
            type="number"
            className="form-input"
            value={(params.period as number) ?? 20}
            min={1}
            max={500}
            onChange={(e) =>
              onUpdate({ params: { ...params, period: Number(e.target.value) } })
            }
          />
        </Field>
      )}
    </>
  );
}

export function FilterForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  const [parseError, setParseError] = useState(false);

  return (
    <>
      <Field label="字段名">
        <input
          type="text"
          className="form-input"
          value={(params.field as string) ?? ''}
          onChange={(e) => onUpdate({ params: { ...params, field: e.target.value } })}
          placeholder="chg_pct / pe / close"
        />
      </Field>
      <Field label="比较操作符">
        <select
          className="form-select"
          value={(params.op as string) ?? '>'}
          onChange={(e) => onUpdate({ params: { ...params, op: e.target.value } })}
        >
          <option value=">">{'>'} 大于</option>
          <option value="<">{'<'} 小于</option>
          <option value=">=">{'>='} 大于等于</option>
          <option value="<=">{'<='} 小于等于</option>
          <option value="==">{'='} 等于</option>
          <option value="!=">{'!='} 不等于</option>
          <option value="between">between 区间</option>
          <option value="in">in 列表</option>
          <option value="contains">contains 包含</option>
          <option value="contains_any">contains_any 含任一</option>
        </select>
      </Field>
      <Field label="比较值">
        <input
          type="text"
          className={`form-input ${parseError ? 'border-rose-400 focus:border-rose-500 focus:ring-rose-200' : ''}`}
          value={JSON.stringify(params.value ?? 0)}
          onChange={(e) => {
            try {
              JSON.parse(e.target.value);
              setParseError(false);
              onUpdate({ params: { ...params, value: JSON.parse(e.target.value) } });
            } catch {
              setParseError(true);
            }
          }}
          placeholder={'0  /  [2, 5]  /  ["600519.SH"]  /  ["分红","重组"]'}
        />
        {parseError && (
          <p className="mt-1 text-[10px] text-rose-600">JSON 格式错误 — 例如 3, [2,5], "text"</p>
        )}
        {!parseError && (
          <p className="mt-1 text-[10px] text-slate-400">
            数字直接写, 区间 [2, 5], 列表/含任一 ["x", "y"], 字符串带引号
          </p>
        )}
      </Field>
    </>
  );
}

function RankForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  return (
    <>
      <Field label="排名字段">
        <input
          type="text"
          className="form-input"
          value={(params.field as string) ?? ''}
          onChange={(e) => onUpdate({ params: { ...params, field: e.target.value } })}
          placeholder="chg_pct / volume_ratio"
        />
      </Field>
      <Field label="排序方向">
        <select
          className="form-select"
          value={(params.order as string) ?? 'desc'}
          onChange={(e) => onUpdate({ params: { ...params, order: e.target.value } })}
        >
          <option value="desc">降序 (从大到小)</option>
          <option value="asc">升序 (从小到大)</option>
        </select>
      </Field>
      <Field label="取 Top N">
        <input
          type="number"
          className="form-input"
          value={(params.top as number) ?? 20}
          min={1}
          max={500}
          onChange={(e) => onUpdate({ params: { ...params, top: Number(e.target.value) } })}
        />
      </Field>
    </>
  );
}

function DedupeForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  return (
    <Field label="去重 key">
      <input
        type="text"
        className="form-input"
        value={(params.key as string) ?? 'stock_code'}
        onChange={(e) => onUpdate({ params: { ...params, key: e.target.value } })}
      />
    </Field>
  );
}

function PersistForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  const extraTags = Array.isArray(params.extra_tags)
    ? (params.extra_tags as string[]).join(',')
    : '';
  return (
    <Field label="额外标签 (逗号分隔)">
      <input
        type="text"
        className="form-input"
        value={extraTags}
        onChange={(e) =>
          onUpdate({
            params: {
              ...params,
              extra_tags: e.target.value
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean),
            },
          })
        }
        placeholder="morning_volume_breakout"
      />
    </Field>
  );
}

function NotifyForm({ node, onUpdate }: { node: RFNode; onUpdate: (p: Partial<RFNode['data']>) => void }): JSX.Element {
  const params = (node.data.params ?? {}) as Record<string, unknown>;
  return (
    <Field label="通知通道">
      <select
        className="form-select"
        value={(params.channel_type as string) ?? 'morning'}
        onChange={(e) => onUpdate({ params: { ...params, channel_type: e.target.value } })}
      >
        <option value="morning">早盘 (morning)</option>
        <option value="afternoon">午后 (afternoon)</option>
        <option value="review">复盘 (review)</option>
        <option value="alert">告警 (alert)</option>
      </select>
    </Field>
  );
}

function EmptyForm({ note }: { note: string }): JSX.Element {
  return <p className="text-xs italic text-slate-400">{note}</p>;
}

// =============================================================================
// Field wrapper
// =============================================================================

function Field({ label, children }: { label: string; children: React.ReactNode }): JSX.Element {
  return (
    <label className="block">
      <span className="text-[11px] font-medium text-slate-600">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
  );
}
