import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
  },
}));

vi.mock('./client', () => ({
  apiClient: {
    get: mocks.get,
  },
}));

import { apiKeyUsageApi, normalizeApiKeyUsagePage } from './apiKeyUsage';

beforeEach(() => {
  mocks.get.mockReset();
});

describe('apiKeyUsageApi', () => {
  it('normalizes bounded page responses', () => {
    expect(
      normalizeApiKeyUsagePage({
        items: [
          {
            provider: 'Codex',
            composite_key: '|key-one',
            success: 3,
            failed: 1,
            recent_requests: [],
          },
        ],
        total: '3',
        page: 2,
        page_size: 1,
        total_pages: 3,
        has_more: true,
      })
    ).toEqual({
      items: [
        {
          provider: 'codex',
          composite_key: '|key-one',
          success: 3,
          failed: 1,
          recent_requests: [],
          recentRequests: undefined,
        },
      ],
      total: 3,
      page: 2,
      page_size: 1,
      total_pages: 3,
      has_more: true,
    });
  });

  it('normalizes legacy nested responses during rolling upgrades', () => {
    const page = normalizeApiKeyUsagePage({
      codex: {
        '|legacy-key': { success: 2, failed: 0, recent_requests: [] },
      },
    });

    expect(page.items).toEqual([
      expect.objectContaining({ provider: 'codex', composite_key: '|legacy-key', success: 2 }),
    ]);
    expect(page.total).toBe(1);
  });

  it('passes server-side filters and pagination', async () => {
    mocks.get.mockResolvedValue({ items: [], total: 0 });

    await apiKeyUsageApi.getUsage({
      page: 2,
      pageSize: 50,
      providers: [' Codex ', 'claude'],
      search: ' team ',
    });

    expect(mocks.get).toHaveBeenCalledWith('/api-key-usage', {
      params: {
        page: 2,
        page_size: 50,
        provider: 'Codex,claude',
        search: 'team',
      },
      timeout: expect.any(Number),
    });
  });

  it('loads every result through bounded pages', async () => {
    mocks.get
      .mockResolvedValueOnce({
        items: [{ provider: 'claude', composite_key: '|one' }],
        total: 2,
        page: 1,
        page_size: 200,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        items: [{ provider: 'codex', composite_key: '|two' }],
        total: 2,
        page: 2,
        page_size: 200,
        total_pages: 2,
        has_more: false,
      });

    await expect(apiKeyUsageApi.getAllUsage()).resolves.toEqual([
      expect.objectContaining({ composite_key: '|one' }),
      expect.objectContaining({ composite_key: '|two' }),
    ]);
    expect(mocks.get).toHaveBeenCalledTimes(2);
    expect(mocks.get.mock.calls[1]?.[1]?.params.page).toBe(2);
  });
});
