import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiKeysCardEditor } from './VisualConfigEditorBlocks';
import { Button } from '@/components/ui/Button';
import { sha256Hex } from '@/utils/apiKeyHash';

vi.mock('@/components/ui/Modal', () => ({
  Modal: ({
    open,
    children,
    footer,
  }: {
    open: boolean;
    children: unknown;
    footer?: unknown;
  }) => (open ? [children, footer] : null),
}));

const { mocks } = vi.hoisted(() => ({
  mocks: {
    showNotification: vi.fn(),
    showConfirmation: vi.fn(),
    getInfo: vi.fn(async () => ({ service: 'cpa-manager' })),
    getApiKeyAliases: vi.fn(async () => ({ items: [] as Array<{ apiKeyHash: string; alias: string }> })),
    saveApiKeyAliases: vi.fn(async () => ({ items: [] as Array<{ apiKeyHash: string; alias: string }> })),
    deleteApiKeyAlias: vi.fn(async () => undefined),
  },
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

vi.mock('@/stores', () => ({
  useNotificationStore: (selector: (state: any) => unknown) =>
    selector({
      showNotification: mocks.showNotification,
      showConfirmation: mocks.showConfirmation,
    }),
  useAuthStore: (selector: (state: any) => unknown) =>
    selector({
      apiBase: 'http://127.0.0.1:8080',
      managementKey: 'mgmt-key',
    }),
  useUsageServiceStore: (selector: (state: any) => unknown) =>
    selector({
      enabled: true,
      serviceBase: 'http://127.0.0.1:19090',
    }),
}));

vi.mock('@/services/api/usageService', () => ({
  isUsageServiceId: () => true,
  normalizeUsageServiceBase: (value: string) => value,
  usageServiceApi: {
    getInfo: mocks.getInfo,
    getApiKeyAliases: mocks.getApiKeyAliases,
    saveApiKeyAliases: mocks.saveApiKeyAliases,
    deleteApiKeyAlias: mocks.deleteApiKeyAlias,
  },
}));

vi.mock('@/utils/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}));

vi.mock('@/utils/connection', () => ({
  detectApiBaseFromLocation: () => '',
}));

describe('ApiKeysCardEditor alias cleanup', () => {
  let renderer: ReactTestRenderer;

  beforeEach(() => {
    vi.clearAllMocks();
    renderer = null as unknown as ReactTestRenderer;
  });

  it('删除 API Key 时会同步删除对应 alias', async () => {
    const apiKey = 'sk-test-key';
    const apiKeyHash = sha256Hex(apiKey).toLowerCase();
    const onChange = vi.fn();

    mocks.getApiKeyAliases.mockResolvedValueOnce({
      items: [{ apiKeyHash, alias: 'Team A' }],
    });

    await act(async () => {
      renderer = create(<ApiKeysCardEditor value={apiKey} onChange={onChange} />);
    });

    const deleteButton = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === 'config_management.visual.common.delete');

    expect(deleteButton).toBeTruthy();

    await act(async () => {
      await deleteButton!.props.onClick();
    });

    expect(mocks.deleteApiKeyAlias).toHaveBeenCalledWith(
      'http://127.0.0.1:19090',
      apiKeyHash,
      'mgmt-key'
    );
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('编辑 API Key 且 hash 变化时会清理旧 alias', async () => {
    const oldKey = 'sk-old-key';
    const newKey = 'sk-new-key';
    const oldHash = sha256Hex(oldKey).toLowerCase();
    const onChange = vi.fn();

    mocks.getApiKeyAliases.mockResolvedValueOnce({
      items: [{ apiKeyHash: oldHash, alias: 'Team A' }],
    });
    await act(async () => {
      renderer = create(<ApiKeysCardEditor value={oldKey} onChange={onChange} />);
    });

    await act(async () => {
      await Promise.resolve();
    });

    await act(async () => {
      await Promise.resolve();
    });

    const editButton = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === 'config_management.visual.common.edit');
    expect(editButton).toBeTruthy();

    await act(async () => {
      editButton!.props.onClick();
    });

    const inputs = renderer.root.findAll((node) => node.type === 'input');
    expect(inputs.length).toBeGreaterThanOrEqual(2);
    const aliasInput = inputs.find((node) => node.props.maxLength === 120);
    expect(aliasInput).toBeTruthy();

    await act(async () => {
      inputs[0].props.onChange({ target: { value: newKey } });
      aliasInput!.props.onChange({ target: { value: '' } });
    });

    const saveButton = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === 'config_management.visual.common.update');
    expect(saveButton).toBeTruthy();

    await act(async () => {
      await saveButton!.props.onClick();
    });

    expect(mocks.deleteApiKeyAlias).toHaveBeenCalledWith(
      'http://127.0.0.1:19090',
      oldHash,
      'mgmt-key'
    );
    expect(onChange).toHaveBeenCalledWith(newKey);
  });

});
