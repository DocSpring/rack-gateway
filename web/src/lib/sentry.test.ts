import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@sentry/react', () => ({
  init: vi.fn(),
  browserTracingIntegration: vi.fn(() => ({
    name: 'browserTracingIntegration',
  })),
  ErrorBoundary: vi.fn(),
}));

const sentry = await import('@sentry/react');
const initMock = sentry.init as unknown as ReturnType<typeof vi.fn>;
const browserTracingIntegrationMock =
  sentry.browserTracingIntegration as unknown as ReturnType<typeof vi.fn>;

const { initSentry, __resetSentryForTests } = await import('./sentry');

function setEnv(key: string, value: string | undefined) {
  if (typeof document === 'undefined') {
    return;
  }
  let meta = document.querySelector(
    `meta[name="${key}"]`,
  ) as HTMLMetaElement | null;
  if (!meta && value !== undefined) {
    meta = document.createElement('meta');
    meta.setAttribute('name', key);
    document.head.append(meta);
  }
  if (!meta) {
    return;
  }
  if (value === undefined) {
    meta.remove();
    return;
  }
  meta.setAttribute('content', value);
}

beforeEach(() => {
  __resetSentryForTests();
  initMock.mockClear();
  browserTracingIntegrationMock.mockClear();
  setEnv('rgw-sentry-dsn', undefined);
  setEnv('rgw-sentry-environment', undefined);
  setEnv('rgw-sentry-release', undefined);
  setEnv('rgw-sentry-traces-sample-rate', undefined);
});

describe('initSentry', () => {
  it('returns false and skips initialization when DSN is missing', () => {
    const enabled = initSentry();
    expect(enabled).toBe(false);
    expect(initMock).not.toHaveBeenCalled();
  });

  it('initializes sentry with sane defaults when DSN provided', () => {
    setEnv('rgw-sentry-dsn', 'https://examplePublicKey@o0.ingest.sentry.io/0');
    setEnv('rgw-sentry-environment', 'test');

    const enabled = initSentry();

    expect(enabled).toBe(true);
    expect(initMock).toHaveBeenCalledTimes(1);
    const options = initMock.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(options.dsn).toBe('https://examplePublicKey@o0.ingest.sentry.io/0');
    expect(options.environment).toBe('test');
    expect(options.release).toBeUndefined();
    expect(options.tracesSampleRate).toBe(0);
    expect(browserTracingIntegrationMock).toHaveBeenCalled();
  });

  it('passes release fallback through to sentry', () => {
    setEnv('rgw-sentry-dsn', 'https://examplePublicKey@o0.ingest.sentry.io/0');
    setEnv('rgw-sentry-release', 'v2024.01.01');

    const enabled = initSentry();

    expect(enabled).toBe(true);
    const options = initMock.mock.calls[0]?.[0] as { release?: string };
    expect(options.release).toBe('v2024.01.01');
  });
});
