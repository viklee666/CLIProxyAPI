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

import { apiKeysApi } from './apiKeys';

beforeEach(() => {
  Object.values(mocks).forEach((mock) => mock.mockReset());
});

describe('apiKeysApi bounded collection protocol', () => {
  it('continues paginated responses and supports a one-record lookup', async () => {
    mocks.get
      .mockResolvedValueOnce({
        'api-keys': ['key-1', 'key-2'],
        page: 1,
        page_size: 2,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        'api-keys': ['key-3'],
        page: 2,
        page_size: 2,
        total_pages: 2,
        has_more: false,
      })
      .mockResolvedValueOnce({ 'api-keys': ['key-1'], has_more: false });

    await expect(apiKeysApi.list()).resolves.toEqual(['key-1', 'key-2', 'key-3']);
    expect(mocks.get).toHaveBeenNthCalledWith(1, '/api-keys');
    expect(mocks.get).toHaveBeenNthCalledWith(2, '/api-keys', {
      params: { page: 2, page_size: 2 },
    });

    await expect(apiKeysApi.first()).resolves.toBe('key-1');
    expect(mocks.get).toHaveBeenNthCalledWith(3, '/api-keys', {
      params: { page: 1, page_size: 1 },
    });
  });

  it('creates one API key without replacing the full list', async () => {
    mocks.post.mockResolvedValue({ status: 'ok' });
    await apiKeysApi.create('new-key');
    expect(mocks.post).toHaveBeenCalledWith('/api-keys', { value: 'new-key' });
    expect(mocks.put).not.toHaveBeenCalled();
  });
});
