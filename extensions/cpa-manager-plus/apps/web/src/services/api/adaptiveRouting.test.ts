import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
    put: vi.fn(),
  },
}));

vi.mock('./client', () => ({
  apiClient: {
    get: mocks.get,
    put: mocks.put,
  },
}));

import { adaptiveRoutingApi, normalizeAdaptiveRoutingSnapshot } from './adaptiveRouting';

beforeEach(() => {
  mocks.get.mockReset();
  mocks.put.mockReset();
});

describe('adaptiveRoutingApi', () => {
  it('normalizes pagination metadata and legacy snapshots', () => {
    expect(
      normalizeAdaptiveRoutingSnapshot({
        enabled: true,
        config: {},
        items: [{ auth_id: 'auth-a' }],
        total: '5',
        page: 2,
        page_size: 1,
        total_pages: 5,
        has_more: true,
      })
    ).toEqual(
      expect.objectContaining({
        enabled: true,
        total: 5,
        page: 2,
        page_size: 1,
        total_pages: 5,
        has_more: true,
        items: [{ auth_id: 'auth-a' }],
      })
    );

    expect(normalizeAdaptiveRoutingSnapshot({ enabled: false, config: {}, items: [] })).toEqual(
      expect.objectContaining({ total: 0, page: 1, total_pages: 0, has_more: false })
    );
  });

  it('passes pagination and server-side score filters', async () => {
    mocks.get.mockResolvedValue({ enabled: true, config: {}, items: [] });

    await adaptiveRoutingApi.getScores({
      provider: ' codex ',
      model: ' gpt-5 ',
      search: ' auth ',
      eligible: true,
      page: 3,
      pageSize: 50,
    });

    expect(mocks.get).toHaveBeenCalledWith('/routing/adaptive/scores', {
      params: {
        provider: 'codex',
        model: 'gpt-5',
        search: 'auth',
        eligible: true,
        page: 3,
        page_size: 50,
      },
    });
  });
});
