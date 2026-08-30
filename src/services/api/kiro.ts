import { apiClient } from './client';

export type KiroAuthMethod = 'builder-id' | 'idc' | 'api_key' | 'refresh_token' | 'external_idp';

export interface KiroConnectRequest {
  state: string;
  method: KiroAuthMethod;
  start_url?: string;
  region?: string;
  api_key?: string;
  refresh_auth_method?: 'builder-id' | 'idc';
  refresh_token?: string;
  client_id?: string;
  client_secret?: string;
  credential_json?: string;
}

export interface KiroConnectResponse {
  status: 'authorization_required' | 'connected';
  url?: string;
  user_code?: string;
}

export const kiroApi = {
  connect: (request: KiroConnectRequest) =>
    apiClient.post<KiroConnectResponse>('/plugins/kiro/connect', request),
};
