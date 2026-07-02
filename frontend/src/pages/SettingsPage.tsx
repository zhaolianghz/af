// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { useEffect, useState } from 'react';
import {
  getProviders,
  saveProviders,
  testProvider,
  type ProviderView,
  type ProviderInput,
} from '@/api/settings';
import { changePassword } from '@/api/auth';
import { useAuthStore } from '@/stores/authStore';
import { notifyError, notifySuccess } from '@/lib/notify';

// OpenAI-compatible presets. Picking one fills base_url + a default
// model; "custom" lets the user type any gateway. All verified
// OpenAI-compatible (2026-06).
const PRESETS: Record<string, { label: string; base_url: string; model: string }> = {
  mock: { label: '内置 Mock(规则式,无需 key)', base_url: '', model: '' },
  deepseek: { label: 'DeepSeek', base_url: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
  minimax: { label: 'MiniMax', base_url: 'https://api.minimax.io/v1', model: 'MiniMax-Text-01' },
  bailian: { label: '阿里云百炼 (通义千问)', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus' },
  glm: { label: '智谱 GLM', base_url: 'https://open.bigmodel.cn/api/paas/v4', model: 'glm-4.5' },
  openai: { label: 'OpenAI', base_url: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
  custom: { label: '自定义(OpenAI 兼容)', base_url: '', model: '' },
};

// Row is the editable client-side shape (api_key plaintext only while
// editing; the server stores + masks it).
interface Row {
  id?: number;
  enabled: boolean;
  preset: string;
  base_url: string;
  api_key: string;
  model: string;
  api_key_set: boolean;
  api_key_masked: string;
}

function viewToRow(v: ProviderView): Row {
  const preset =
    v.provider === 'mock' || v.provider === ''
      ? 'mock'
      : Object.entries(PRESETS).find(([k, x]) => k === v.provider || x.base_url === v.base_url)?.[0] ?? 'custom';
  return {
    id: v.id,
    enabled: v.enabled,
    preset,
    base_url: v.base_url,
    api_key: '',
    model: v.model,
    api_key_set: v.api_key_set,
    api_key_masked: v.api_key_masked,
  };
}

function rowToInput(r: Row): ProviderInput {
  return {
    id: r.id,
    enabled: r.enabled,
    provider: r.preset === 'mock' ? 'mock' : r.preset,
    base_url: r.base_url,
    api_key: r.api_key,
    model: r.model,
    keep_key: r.api_key === '' && r.api_key_set,
  };
}

function blankRow(): Row {
  return { enabled: true, preset: 'mock', base_url: '', api_key: '', model: '', api_key_set: false, api_key_masked: '' };
}

export default function SettingsPage(): JSX.Element {
  const [rows, setRows] = useState<Row[]>([]);
  const [active, setActive] = useState<string>('—');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    getProviders()
      .then((v) => {
        setRows(v.providers.length ? v.providers.map(viewToRow) : [blankRow()]);
        setActive(v.active);
      })
      .catch((e) => notifyError(e, '加载设置失败'));
  }, []);

  const update = (i: number, patch: Partial<Row>) => {
    setRows((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  };
  const choosePreset = (i: number, preset: string) => {
    const p = PRESETS[preset];
    update(i, preset !== 'custom' && p ? { preset, base_url: p.base_url, model: p.model } : { preset });
  };
  const move = (i: number, dir: -1 | 1) => {
    setRows((rs) => {
      const j = i + dir;
      if (j < 0 || j >= rs.length) return rs;
      const next = [...rs];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  };
  const remove = (i: number) => setRows((rs) => rs.filter((_, idx) => idx !== i));
  const add = () => setRows((rs) => [...rs, blankRow()]);

  const save = async () => {
    setBusy(true);
    try {
      const v = await saveProviders(rows.map(rowToInput));
      setRows(v.providers.length ? v.providers.map(viewToRow) : [blankRow()]);
      setActive(v.active);
      notifySuccess(`已保存,当前链:${v.active}`);
    } catch (e) {
      notifyError(e, '保存失败');
    } finally {
      setBusy(false);
    }
  };

  const testOne = async (i: number) => {
    setBusy(true);
    try {
      await testProvider(rowToInput(rows[i]));
      notifySuccess(`第 ${i + 1} 个服务商连接正常`);
    } catch (e) {
      notifyError(e, `第 ${i + 1} 个服务商连接失败`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-slate-900">设置 / Settings</h1>
        <p className="mt-1 text-sm text-slate-500">
          配置 AI 助手与自动复盘的大模型。支持多个服务商,按顺序降级:排在前面的优先,
          不可用时(报错/超时/限流)自动切到下一个。修改即时生效,无需重启。
        </p>
      </div>

      <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-soft">
        <div className="flex items-center justify-between border-b border-slate-100 pb-4">
          <div className="text-sm font-semibold text-slate-800">大模型降级链 (LLM fallback chain)</div>
          <div className="text-xs text-slate-400">
            当前: <span className="font-mono text-slate-600">{active}</span>
          </div>
        </div>

        <div className="mt-4 space-y-4">
          {rows.map((r, i) => (
            <ProviderRow
              key={r.id ?? `new-${i}`}
              row={r}
              index={i}
              total={rows.length}
              busy={busy}
              onChoosePreset={(p) => choosePreset(i, p)}
              onUpdate={(patch) => update(i, patch)}
              onMove={(d) => move(i, d)}
              onRemove={() => remove(i)}
              onTest={() => testOne(i)}
            />
          ))}

          <div className="flex items-center justify-between pt-2">
            <button onClick={add} className="text-sm text-brand-600 hover:underline">
              + 添加服务商
            </button>
            <button onClick={save} disabled={busy} className="btn-primary text-sm">
              {busy ? '处理中…' : '保存全部'}
            </button>
          </div>
          <p className="text-xs text-slate-400">
            DeepSeek / MiniMax / 阿里云百炼 / 智谱 GLM / OpenAI 及任意 OpenAI 兼容网关均可。
            顺序即优先级,用上移/下移调整。
          </p>
        </div>
      </div>

      <ChangePasswordCard />
    </div>
  );
}

// ChangePasswordCard is shown only when auth is active (a user is logged
// in). When auth is disabled there's no session and nothing to change.
function ChangePasswordCard(): JSX.Element | null {
  const user = useAuthStore((s) => s.user);
  const [oldPw, setOldPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [confirmPw, setConfirmPw] = useState('');
  const [busy, setBusy] = useState(false);

  if (!user) return null; // auth disabled → no card

  const submit = async () => {
    if (newPw.length < 8) {
      notifyError(new Error('新密码至少 8 位'), '密码太短');
      return;
    }
    if (newPw !== confirmPw) {
      notifyError(new Error('两次输入的新密码不一致'), '请检查');
      return;
    }
    setBusy(true);
    try {
      await changePassword(oldPw, newPw);
      setOldPw('');
      setNewPw('');
      setConfirmPw('');
      notifySuccess('密码已修改,下次登录请用新密码');
    } catch (e) {
      notifyError(e, '修改密码失败');
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = oldPw !== '' && newPw !== '' && confirmPw !== '' && !busy;

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-soft">
      <div className="flex items-center justify-between border-b border-slate-100 pb-4">
        <div className="text-sm font-semibold text-slate-800">修改密码</div>
        <div className="text-xs text-slate-400">
          当前账号: <span className="font-mono text-slate-600">{user.username}</span>
        </div>
      </div>
      <div className="mt-4 max-w-sm space-y-4">
        <Field label="当前密码">
          <input
            type="password"
            value={oldPw}
            onChange={(e) => setOldPw(e.target.value)}
            autoComplete="current-password"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </Field>
        <Field label="新密码(至少 8 位)">
          <input
            type="password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            autoComplete="new-password"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </Field>
        <Field label="确认新密码">
          <input
            type="password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            autoComplete="new-password"
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </Field>
        <button onClick={submit} disabled={!canSubmit} className="btn-primary text-sm">
          {busy ? '处理中…' : '修改密码'}
        </button>
      </div>
    </div>
  );
}

function ProviderRow({
  row,
  index,
  total,
  busy,
  onChoosePreset,
  onUpdate,
  onMove,
  onRemove,
  onTest,
}: {
  row: Row;
  index: number;
  total: number;
  busy: boolean;
  onChoosePreset: (p: string) => void;
  onUpdate: (patch: Partial<Row>) => void;
  onMove: (dir: -1 | 1) => void;
  onRemove: () => void;
  onTest: () => void;
}): JSX.Element {
  const isMock = row.preset === 'mock';
  return (
    <div className="rounded-xl border border-slate-200 p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-500">
            #{index + 1}
          </span>
          <label className="flex items-center gap-1.5 text-xs text-slate-700">
            <input type="checkbox" checked={row.enabled} onChange={(e) => onUpdate({ enabled: e.target.checked })} />
            启用
          </label>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <button onClick={() => onMove(-1)} disabled={index === 0} className="text-slate-400 hover:text-slate-700 disabled:opacity-30">↑</button>
          <button onClick={() => onMove(1)} disabled={index === total - 1} className="text-slate-400 hover:text-slate-700 disabled:opacity-30">↓</button>
          <button onClick={onRemove} className="text-red-500 hover:underline">删除</button>
        </div>
      </div>

      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="服务商">
          <select
            value={row.preset}
            onChange={(e) => onChoosePreset(e.target.value)}
            className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm"
          >
            {Object.entries(PRESETS).map(([k, v]) => (
              <option key={k} value={k}>{v.label}</option>
            ))}
          </select>
        </Field>
        {!isMock && (
          <Field label="模型">
            <input
              value={row.model}
              onChange={(e) => onUpdate({ model: e.target.value })}
              placeholder="deepseek-chat"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono"
            />
          </Field>
        )}
      </div>

      {!isMock && (
        <div className="mt-3 grid grid-cols-1 gap-3">
          <Field label="Base URL">
            <input
              value={row.base_url}
              onChange={(e) => onUpdate({ base_url: e.target.value })}
              placeholder="https://api.deepseek.com/v1"
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono"
            />
          </Field>
          <Field label="API Key">
            <input
              type="password"
              value={row.api_key}
              onChange={(e) => onUpdate({ api_key: e.target.value })}
              placeholder={row.api_key_set ? `已保存 ${row.api_key_masked}(留空则不变)` : 'sk-...'}
              className="w-full rounded-lg border border-slate-300 px-3 py-2 text-sm font-mono"
            />
          </Field>
          <div>
            <button onClick={onTest} disabled={busy} className="btn-secondary text-xs">
              测试此服务商
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }): JSX.Element {
  // Wrapping the control in the <label> associates them for
  // accessibility (and getByLabelText in tests) without needing ids.
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-slate-600">{label}</span>
      {children}
    </label>
  );
}
