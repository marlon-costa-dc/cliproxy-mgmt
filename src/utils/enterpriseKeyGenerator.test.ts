import { describe, expect, it } from 'vitest';
import {
  generateEnterpriseApiKey,
  isValidDepartmentPrefix,
  normalizeDepartmentPrefix
} from './enterpriseKeyGenerator';

describe('enterpriseKeyGenerator', () => {
  it('normalizes and validates department prefix', () => {
    expect(normalizeDepartmentPrefix(' SH ')).toBe('sh');
    expect(isValidDepartmentPrefix('sh')).toBe(true);
    expect(isValidDepartmentPrefix('sh-head')).toBe(true);
    expect(isValidDepartmentPrefix('sh_head')).toBe(false);
  });

  it('generates key with department prefix', () => {
    const key = generateEnterpriseApiKey({
      prefix: 'sh',
      randomLength: 6,
      randomSegmentFactory: () => 'abc123'
    });
    expect(key).toBe('sh-abc123');
  });

  it('retries on conflict and returns unique value', () => {
    const parts = ['dup000', 'uniq01'];
    const key = generateEnterpriseApiKey({
      prefix: 'sh',
      randomLength: 6,
      existingKeys: ['sh-dup000'],
      randomSegmentFactory: () => parts.shift() || 'fallback'
    });
    expect(key).toBe('sh-uniq01');
  });
});

