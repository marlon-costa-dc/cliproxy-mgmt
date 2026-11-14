import { describe, expect, test } from 'bun:test';
import {
  qoderRemainingPercent,
  readQoderUsageSnapshot,
} from '../src/features/quota/providers/qoder/data';

describe('Qoder quota integration', () => {
  test('reads the backend usage snapshot from an auth file', () => {
    const usage = { used: 25, total: 100, unit: 'credits' };
    expect(readQoderUsageSnapshot({ name: 'qoder.json', type: 'qoder', usage })).toEqual(usage);
    expect(readQoderUsageSnapshot({ name: 'qoder.json', type: 'qoder' })).toBeNull();
  });

  test('normalizes explicit and derived remaining percentages', () => {
    expect(qoderRemainingPercent({ percentage: 0.25 })).toBe(75);
    expect(qoderRemainingPercent({ percentage: 25 })).toBe(75);
    expect(qoderRemainingPercent({ used: 40, total: 100 })).toBe(60);
    expect(qoderRemainingPercent({ remaining: 10, total: 40 })).toBe(25);
  });
});
