import { act, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ModelInputList } from '@/components/ui/ModelInputList';
import { CodexEditDrawer } from './CodexEditDrawer';

const mocks = vi.hoisted(() => ({
  t: (key: string) => key,
  getCodexConfigs: vi.fn(),
  updateConfigValue: vi.fn(),
  clearCache: vi.fn(),
  showNotification: vi.fn(),
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mocks.t,
  }),
}));

vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? <div>{children}</div> : null,
}));

vi.mock('@/services/api', () => ({
  apiCallApi: { request: vi.fn() },
  getApiCallErrorMessage: vi.fn(),
  modelsApi: { fetchV1ModelsViaApiCall: vi.fn() },
  providersApi: {
    getCodexConfigs: mocks.getCodexConfigs,
  },
}));

vi.mock('@/stores', () => ({
  useConfigStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      updateConfigValue: mocks.updateConfigValue,
      clearCache: mocks.clearCache,
    }),
  useNotificationStore: () => ({ showNotification: mocks.showNotification }),
}));

describe('CodexEditDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('loads full provider records so saved custom models are shown when reopened', async () => {
    mocks.getCodexConfigs.mockResolvedValue([
      {
        apiKey: 'codex-key',
        baseUrl: 'https://codex.example/v1',
        models: [{ name: 'gpt-5.5-custom', alias: 'custom-codex' }],
      },
    ]);

    let renderer!: ReactTestRenderer;
    await act(async () => {
      renderer = create(
        <CodexEditDrawer
          open
          editIndex={0}
          disabled={false}
          onClose={vi.fn()}
          onSaved={vi.fn()}
        />
      );
      await Promise.resolve();
    });

    expect(mocks.getCodexConfigs).toHaveBeenCalledTimes(1);
    expect(renderer.root.findByType(ModelInputList).props.entries).toEqual([
      { name: 'gpt-5.5-custom', alias: 'custom-codex' },
    ]);

    act(() => renderer.unmount());
  });
});
