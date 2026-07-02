// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * ErrorBoundary — root-level crash barrier. Wraps the entire router
 * tree so a single render crash doesn't white-screen the application.
 * Falls back to "something went wrong" + reload button.
 */
import { Component, type ErrorInfo, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Keep structured logs in the console so we can reconstruct the crash.
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary]', error.message, info.componentStack ?? '');
  }

  render(): ReactNode {
    if (this.state.hasError) {
      return (
        <div className="flex h-screen flex-col items-center justify-center gap-4 bg-slate-50">
          <div className="rounded-2xl border border-rose-200 bg-white p-8 text-center shadow-soft">
            <h1 className="text-lg font-semibold text-slate-900">应用遇到了一个错误</h1>
            <p className="mt-2 max-w-md text-sm text-slate-500">
              {this.state.error?.message ?? '未知错误'}
            </p>
            <button
              type="button"
              className="btn-primary mt-6"
              onClick={() => window.location.reload()}
            >
              刷新页面
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
