/**
 * Self-service usage API client.
 * Uses direct fetch (not apiClient) because the endpoint authenticates via the API key itself,
 * not the management key.
 */

export interface SelfUsageQuotaStatus {
  paused: boolean;
  paused_reason: string;
  resumes_at: string | null;
}

export interface SelfUsagePeriod {
  requests: number;
  success: number;
  failed: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  total_tokens: number;
  cost_cents: number;
  limit_cents: number;
}

export interface SelfModelUsage {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cost_cents: number;
}

export interface SelfUsageData {
  today: SelfUsagePeriod;
  this_week: SelfUsagePeriod;
  by_model: Record<string, SelfModelUsage>;
  available: boolean;
}

export interface SelfUsageResponse {
  status: string;
  quota: SelfUsageQuotaStatus;
  usage: SelfUsageData;
}

export async function querySelfUsage(apiBase: string, apiKey: string): Promise<SelfUsageResponse> {
  const base = apiBase.replace(/\/+$/, '');
  const url = `${base}/v1/usage/self`;

  const response = await fetch(url, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
    },
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Request failed with status ${response.status}`);
  }

  return response.json();
}
