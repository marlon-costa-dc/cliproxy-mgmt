import type { AuthFileItem } from '@/types';
import {
  isCodexFile,
  normalizeAuthIndex,
  resolveCodexChatgptAccountId,
  resolveCodexPlanType,
} from '@/utils/quota';
import type { MonitoringAccountAuthState } from './accountOverviewState';
import type { MonitoringAccountRow } from './hooks/useMonitoringData';

export type MonitoringAccountQuotaTarget = {
  key: string;
  authIndex: string;
  authLabel: string;
  fileName: string;
  accountId: string | null;
  planType: string | null;
};

const readAuthFileQuotaLabel = (file: AuthFileItem, authIndex: string) => {
  const candidates = [file.label, file.name, file.email, file.account, authIndex];
  for (const candidate of candidates) {
    const text =
      typeof candidate === 'string'
        ? candidate.trim()
        : candidate === null || candidate === undefined
          ? ''
          : String(candidate).trim();
    if (text) return text;
  }
  return authIndex;
};

export const buildMonitoringAccountQuotaTargetsByAccount = (
  rows: MonitoringAccountRow[],
  authStateByRowId: Map<string, MonitoringAccountAuthState>
) =>
  new Map(
    rows.map((row) => {
      const bucket = new Map<string, MonitoringAccountQuotaTarget>();
      const authState = authStateByRowId.get(row.id);

      authState?.files.forEach((file) => {
        const authIndex = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
        if (!authIndex || !isCodexFile(file)) return;

        const dedupeKey = `${authIndex}::${file.name}`;
        if (bucket.has(dedupeKey)) return;

        bucket.set(dedupeKey, {
          key: dedupeKey,
          authIndex,
          authLabel: readAuthFileQuotaLabel(file, authIndex),
          fileName: file.name,
          accountId: resolveCodexChatgptAccountId(file),
          planType: resolveCodexPlanType(file),
        });
      });

      return [
        row.account,
        Array.from(bucket.values()).sort((left, right) =>
          left.authLabel.localeCompare(right.authLabel)
        ),
      ] as const;
    })
  );
