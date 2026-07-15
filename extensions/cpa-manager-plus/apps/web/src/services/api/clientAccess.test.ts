import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('./client', () => ({ apiClient: mocks }));

import { clientAccessApi } from './clientAccess';

beforeEach(() => {
  Object.values(mocks).forEach((mock) => mock.mockReset());
});

describe('clientAccessApi pagination helpers', () => {
  it('loads every group page instead of stopping at 200', async () => {
    mocks.get
      .mockResolvedValueOnce({ items: [{ id: 1 }], total: 201, page: 1, page_size: 200 })
      .mockResolvedValueOnce({ items: [{ id: 201 }], total: 201, page: 2, page_size: 200 });

    await expect(clientAccessApi.listAllGroups()).resolves.toEqual([{ id: 1 }, { id: 201 }]);
    expect(mocks.get).toHaveBeenNthCalledWith(
      2,
      '/client-access/groups',
      expect.objectContaining({ params: expect.objectContaining({ page: 2, page_size: 200 }) })
    );
  });

  it('follows the server page_size when bindings requests are clamped', async () => {
    mocks.get
      .mockResolvedValueOnce({
        items: [{ auth_index: 'auth-1', group_id: 1 }],
        total: 201,
        page: 1,
        page_size: 200,
      })
      .mockResolvedValueOnce({
        items: [{ auth_index: 'auth-1', group_id: 201 }],
        total: 201,
        page: 2,
        page_size: 200,
      });

    await expect(clientAccessApi.listAllCredentialBindings(['auth-1'])).resolves.toHaveLength(2);
    expect(mocks.get).toHaveBeenNthCalledWith(
      2,
      '/client-access/credential-bindings',
      expect.objectContaining({ params: expect.objectContaining({ page: 2, page_size: 500 }) })
    );
  });

  it('sends query selectors without expanding auth indices', async () => {
    mocks.post.mockResolvedValue({ matched: 10, updated: 8, unchanged: 2, excluded: 1 });
    const selection = {
      mode: 'query' as const,
      all: false,
      providers: ['codex'],
      plan_types: ['team'],
      excluded_auth_indices: ['auth-2'],
    };

    await clientAccessApi.bulkReplaceCredentialBindings(selection, [{ group_id: 1, priority: 10 }]);

    expect(mocks.post).toHaveBeenCalledWith('/client-access/credential-bindings/bulk', {
      selection,
      groups: [{ group_id: 1, priority: 10 }],
      dry_run: false,
    });
  });
});
