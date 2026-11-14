import { describe, expect, test } from 'bun:test';
import {
  createLatestUsageRequestGuard,
  runLatestUsageRequest,
} from '../src/pages/usage-page-request-guard';

const deferred = <T>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
};

describe('UsagePage request ordering', () => {
  test('only the newest concurrent request may commit state or clear loading', () => {
    const guard = createLatestUsageRequestGuard();
    const first = guard.begin();
    const second = guard.begin();

    expect(guard.isLatest(first)).toBe(false);
    expect(guard.isLatest(second)).toBe(true);
  });

  test('invalidates an in-flight request when the page unmounts', () => {
    const guard = createLatestUsageRequestGuard();
    const request = guard.begin();

    guard.invalidate();

    expect(guard.isLatest(request)).toBe(false);
  });

  test('a stale failure is not eligible for propagation after a newer success', () => {
    const guard = createLatestUsageRequestGuard();
    const staleFailure = guard.begin();
    const newerSuccess = guard.begin();

    expect(guard.isLatest(newerSuccess)).toBe(true);
    expect(guard.isLatest(staleFailure)).toBe(false);
  });

  test('newer success suppresses an older failure across the page request orchestration', async () => {
    const guard = createLatestUsageRequestGuard();
    const first = deferred<string>();
    const second = deferred<string>();
    const successes: string[] = [];
    const errors: string[] = [];
    let finishes = 0;
    const run = (request: () => Promise<string>) =>
      runLatestUsageRequest({
        guard,
        request,
        fallbackError: 'load failed',
        onStart: () => undefined,
        onSuccess: (payload) => successes.push(payload),
        onError: (message) => errors.push(message),
        onFinish: () => {
          finishes += 1;
        },
      });

    const staleRun = run(() => first.promise);
    const latestRun = run(() => second.promise);
    second.resolve('latest');
    await latestRun;
    first.reject(new Error('stale failure'));

    await expect(staleRun).resolves.toBeUndefined();
    expect(successes).toEqual(['latest']);
    expect(errors).toEqual([]);
    expect(finishes).toBe(1);
  });

  test('current failure renders its message and rejects to the global refresh caller', async () => {
    const errors: string[] = [];
    let finishes = 0;
    const failure = runLatestUsageRequest({
      guard: createLatestUsageRequestGuard(),
      request: () => Promise.reject(new Error('current failure')),
      fallbackError: 'load failed',
      onStart: () => undefined,
      onSuccess: () => undefined,
      onError: (message) => errors.push(message),
      onFinish: () => {
        finishes += 1;
      },
    });

    await expect(failure).rejects.toThrow('current failure');
    expect(errors).toEqual(['current failure']);
    expect(finishes).toBe(1);
  });
});
