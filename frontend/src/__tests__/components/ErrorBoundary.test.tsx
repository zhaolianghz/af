// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import ErrorBoundary from '@/components/shared/ErrorBoundary';

// A component that throws on demand.
function Bomb({ throwOn }: { throwOn: boolean }): JSX.Element {
  if (throwOn) throw new Error('Boom');
  return <div>ok</div>;
}

describe('ErrorBoundary', () => {
  let consoleSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    consoleSpy.mockRestore();
  });

  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <div>hello</div>
      </ErrorBoundary>,
    );
    expect(screen.getByText('hello')).toBeInTheDocument();
  });

  it('renders fallback UI when child throws', () => {
    render(
      <ErrorBoundary>
        <Bomb throwOn={true} />
      </ErrorBoundary>,
    );
    expect(screen.getByText('应用遇到了一个错误')).toBeInTheDocument();
    expect(screen.getByText('Boom')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '刷新页面' })).toBeInTheDocument();
  });

  it('logs the error to console.error with component stack', () => {
    render(
      <ErrorBoundary>
        <Bomb throwOn={true} />
      </ErrorBoundary>,
    );
    expect(consoleSpy).toHaveBeenCalled();
    // componentDidCatch calls console.error with a tag, the Error, and the stack.
    const flatCalls = consoleSpy.mock.calls.map((c) => c.map((a) => String(a)).join(' ')).join('\n');
    expect(flatCalls).toContain('ErrorBoundary');
    expect(flatCalls).toContain('Boom');
  });

  it('does NOT render children when fallback is shown', () => {
    render(
      <ErrorBoundary>
        <Bomb throwOn={true} />
      </ErrorBoundary>,
    );
    expect(screen.queryByText('ok')).not.toBeInTheDocument();
  });
});
