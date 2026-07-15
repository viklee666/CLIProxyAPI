import { beforeEach, describe, expect, it, vi } from 'vitest';

const { get } = vi.hoisted(() => ({ get: vi.fn() }));

vi.mock('./client', () => ({
  apiClient: {
    get,
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

import { configApi } from './config';

beforeEach(() => {
  get.mockReset();
});

describe('configApi summary', () => {
  it('loads non-secret dashboard counts from the summary endpoint', async () => {
    get.mockResolvedValue({
      api_keys: '2',
      gemini_api_keys: 3,
      codex_api_keys: 4,
      claude_api_keys: 5,
      openai_compatibility: 6,
      provider_credentials: 18,
    });

    await expect(configApi.getSummary()).resolves.toEqual({
      apiKeys: 2,
      geminiApiKeys: 3,
      interactionsApiKeys: 0,
      codexApiKeys: 4,
      claudeApiKeys: 5,
      xaiApiKeys: 0,
      vertexApiKeys: 0,
      openAICompatibility: 6,
      providerCredentials: 18,
    });
    expect(get).toHaveBeenCalledWith('/config/summary');
  });
});

describe('configApi UI projection', () => {
  it('uses the bounded UI view for routine config bootstrap', async () => {
    get.mockResolvedValue({ debug: true, projection: 'ui' });

    const result = await configApi.getConfig();

    expect(result.debug).toBe(true);
    expect(get).toHaveBeenCalledWith('/config', { params: { view: 'ui' } });
  });
});
