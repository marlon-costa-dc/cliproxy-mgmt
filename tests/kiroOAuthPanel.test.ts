import { afterEach, describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { apiClient, kiroApi, type KiroConnectRequest } from '../src/services/api';

const originalPost = apiClient.post;

afterEach(() => {
  apiClient.post = originalPost;
});

describe('Kiro OAuth flat panel integration', () => {
  test('submits credentials through the authenticated plugin management route', async () => {
    let capturedURL = '';
    let capturedBody: unknown;
    apiClient.post = (async (url: string, body: unknown) => {
      capturedURL = url;
      capturedBody = body;
      return { status: 'connected' };
    }) as typeof apiClient.post;

    const request: KiroConnectRequest = {
      state: 'state-value',
      method: 'api_key',
      region: 'us-east-1',
      api_key: 'secret',
    };
    await kiroApi.connect(request);

    expect(capturedURL).toBe('/plugins/kiro/connect');
    expect(capturedBody).toEqual(request);
  });

  test('OAuth page renders Kiro through its dedicated inline card', () => {
    const source = readFileSync(new URL('../src/pages/OAuthPage.tsx', import.meta.url), 'utf8');
    expect(source).toContain("provider.id === 'kiro'");
    expect(source).toContain('<KiroOAuthCard');
  });

  test('Kiro panel does not reference the retired standalone resource page', () => {
    const source = readFileSync(
      new URL('../src/features/kiro/KiroOAuthCard.tsx', import.meta.url),
      'utf8'
    );
    expect(source).not.toContain('/v0/resource/plugins/kiro');
    expect(source).not.toContain('callbackUrl');
  });

  test('OAuth polling is sequential so device-code requests cannot overlap', () => {
    const source = readFileSync(
      new URL('../src/features/kiro/KiroOAuthCard.tsx', import.meta.url),
      'utf8'
    );
    expect(source).not.toContain('window.setInterval');
    expect(source).toContain('window.setTimeout(poll, 3000)');
  });
});
