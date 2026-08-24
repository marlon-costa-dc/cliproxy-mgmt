const BASE62 = '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ';

const PREFIX_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export const normalizeDepartmentPrefix = (value: string): string => value.trim().toLowerCase();

export const isValidDepartmentPrefix = (value: string): boolean => PREFIX_RE.test(value);

const randomBase62 = (length: number): string => {
  let output = '';
  for (let index = 0; index < length; index += 1) {
    output += BASE62[Math.floor(Math.random() * BASE62.length)];
  }
  return output;
};

export interface GenerateEnterpriseApiKeyOptions {
  prefix: string;
  randomLength?: number;
  maxRetries?: number;
  existingKeys?: Iterable<string>;
  randomSegmentFactory?: (length: number) => string;
}

export const generateEnterpriseApiKey = ({
  prefix,
  randomLength = 8,
  maxRetries = 5,
  existingKeys = [],
  randomSegmentFactory = randomBase62
}: GenerateEnterpriseApiKeyOptions): string => {
  const normalizedPrefix = normalizeDepartmentPrefix(prefix);
  if (!isValidDepartmentPrefix(normalizedPrefix)) {
    throw new Error(`Invalid department prefix: ${prefix}`);
  }
  if (!Number.isInteger(randomLength) || randomLength < 4 || randomLength > 32) {
    throw new Error(`Invalid randomLength: ${randomLength}`);
  }

  const keySet = new Set(Array.from(existingKeys, (item) => item.trim()).filter(Boolean));
  for (let retry = 0; retry < maxRetries; retry += 1) {
    const candidate = `${normalizedPrefix}-${randomSegmentFactory(randomLength)}`;
    if (!keySet.has(candidate)) {
      return candidate;
    }
  }

  throw new Error('Failed to generate unique API key after max retries');
};

