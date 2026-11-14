import {
  mergeRecentRequestBucketGroups,
  normalizeRecentRequestUsageEntry,
  type ApiKeyUsageResponse,
  type RecentRequestBucket,
} from '@/utils/recentRequests';

export interface UsageProviderRow {
  provider: string;
  credentials: number;
  success: number;
  failures: number;
}

export interface UsageSummary {
  success: number;
  failures: number;
  providers: UsageProviderRow[];
  recent: RecentRequestBucket[];
}

const HTTP_ROUTE_RE = /^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+\//i;
const HASHED_API_KEY_RE = /^api-key:[0-9a-f]{8}$/i;
const SAFE_IDENTIFIER_RE =
  /^(unknown|other|gemini|gemini-cli|aistudio|vertex|claude|codex|openai|openai-compatibility|openai-compatible|antigravity|github-copilot|gitlab|cursor|kiro|kilo|kimi|iflow|codebuddy)$/i;

const maskSecretSegment = (value: string): string => {
  const trimmed = value.trim();
  if (!trimmed) return '';
  if (trimmed.length <= 8) return '***';
  return `${trimmed.slice(0, 6)}...${trimmed.slice(-4)}`;
};

export function maskUsageIdentifier(value: string): string {
  const label = value.trim();
  if (!label) return 'unknown';
  if (HTTP_ROUTE_RE.test(label) || label.startsWith('/')) return label;
  if (HASHED_API_KEY_RE.test(label)) return label;
  if (SAFE_IDENTIFIER_RE.test(label)) return label;

  if (label.includes('|')) {
    const parts = label.split('|');
    const secret = parts.pop() ?? '';
    return `${parts.join('|')}|${maskSecretSegment(secret)}`;
  }

  return maskSecretSegment(label);
}

export function formatLargeNumber(value: number): string {
  return new Intl.NumberFormat(undefined, {
    notation: value >= 100_000 ? 'compact' : 'standard',
    maximumFractionDigits: value >= 100_000 ? 1 : 0,
  }).format(value);
}

export function summarizeApiKeyUsage(payload: ApiKeyUsageResponse | null | undefined): UsageSummary {
  const providers: UsageProviderRow[] = [];
  const buckets: RecentRequestBucket[][] = [];
  Object.entries(payload ?? {}).forEach(([provider, entries]) => {
    if (!entries || typeof entries !== 'object' || Array.isArray(entries)) return;
    const row: UsageProviderRow = {
      provider: maskUsageIdentifier(provider),
      credentials: 0,
      success: 0,
      failures: 0,
    };
    Object.values(entries).forEach((raw) => {
      const entry = normalizeRecentRequestUsageEntry(raw);
      row.credentials += 1;
      row.success += entry.success;
      row.failures += entry.failed;
      buckets.push(entry.recentRequests);
    });
    providers.push(row);
  });
  providers.sort((a, b) => b.success + b.failures - (a.success + a.failures));
  return {
    success: providers.reduce((sum, row) => sum + row.success, 0),
    failures: providers.reduce((sum, row) => sum + row.failures, 0),
    providers,
    recent: mergeRecentRequestBucketGroups(buckets),
  };
}
