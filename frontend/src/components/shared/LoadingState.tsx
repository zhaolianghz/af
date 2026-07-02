// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * LoadingState — centered spinner + label for async-first-render.
 */
export default function LoadingState({ label = '加载中…' }: { label?: string }): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-slate-400">
      <svg
        className="h-6 w-6 animate-spin text-slate-300"
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
      >
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path
          className="opacity-75"
          fill="currentColor"
          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
        />
      </svg>
      <span className="text-xs">{label}</span>
    </div>
  );
}
