import { normalizeUsageServiceBase } from './usageService';

// AlertSMTPConfig mirrors the backend store.AlertConfigStored type.
export interface AlertSMTPConfig {
  smtpHost: string;
  smtpPort: number;
  smtpUsername: string;
  smtpPassword: string;
  smtpFrom: string;
  smtpFromName: string;
  alertEnabled: boolean;
  thresholdCents: number;
  checkIntervalMs: number;
  poolCheckEnabled: boolean;
  poolCheckInterval: number;
}

const authHeader = (managementKey?: string): Record<string, string> =>
  managementKey ? { Authorization: `Bearer ${managementKey}` } : {};

const buildUrl = (base: string, path: string): string => {
  const normalized = normalizeUsageServiceBase(base).replace(/\/+$/, '');
  return `${normalized}${path}`;
};

export const alertConfigApi = {
  getConfig: async (base: string, managementKey?: string): Promise<AlertSMTPConfig> => {
    const response = await fetch(buildUrl(base, '/v0/management/alert/config'), {
      headers: { ...authHeader(managementKey) },
    });
    if (!response.ok) {
      throw new Error(`Alert config request failed: HTTP ${response.status}`);
    }
    return response.json();
  },

  updateConfig: async (base: string, cfg: AlertSMTPConfig, managementKey?: string): Promise<AlertSMTPConfig> => {
    const response = await fetch(buildUrl(base, '/v0/management/alert/config'), {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        ...authHeader(managementKey),
      },
      body: JSON.stringify(cfg),
    });
    if (!response.ok) {
      throw new Error(`Alert config update failed: HTTP ${response.status}`);
    }
    return response.json();
  },
};
