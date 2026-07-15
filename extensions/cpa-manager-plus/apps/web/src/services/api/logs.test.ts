import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
    delete: vi.fn(),
    getRaw: vi.fn(),
  },
}));

vi.mock('./client', () => ({
  apiClient: {
    get: mocks.get,
    delete: mocks.delete,
    getRaw: mocks.getRaw,
  },
}));

import { logsApi, normalizeErrorLogsResponse, normalizeLogsResponse } from './logs';

beforeEach(() => {
  mocks.get.mockReset();
  mocks.delete.mockReset();
  mocks.getRaw.mockReset();
});

describe('logs API', () => {
  it('normalizes legacy timestamp-based log responses', () => {
    expect(
      normalizeLogsResponse({
        lines: ['a', 'b'],
        'line-count': 2,
        'latest-timestamp': 123,
      })
    ).toEqual({
      lines: ['a', 'b'],
      'line-count': 2,
      'latest-timestamp': 123,
      latestAfter: 123,
      nextCursor: undefined,
      cursorReset: false,
    });
  });

  it('normalizes cursor-based log responses', () => {
    expect(
      normalizeLogsResponse({
        lines: ['next'],
        lineCount: '1',
        latestAfter: '456',
        'next-cursor': 'cursor-2',
        'cursor-reset': 'true',
      })
    ).toEqual({
      lines: ['next'],
      'line-count': 1,
      'latest-timestamp': 0,
      latestAfter: 456,
      nextCursor: 'cursor-2',
      cursorReset: true,
    });
  });

  it('passes cursor, after, and limit query params when fetching logs', async () => {
    mocks.get.mockResolvedValue({ lines: [] });

    await logsApi.fetchLogs({ cursor: 'cursor-1', after: 123, limit: 100 });

    expect(mocks.get).toHaveBeenCalledWith('/logs', {
      params: { cursor: 'cursor-1', after: 123, limit: 100 },
      timeout: expect.any(Number),
    });
  });

  it('normalizes paged error-log responses', () => {
    expect(
      normalizeErrorLogsResponse({
        files: [{ name: 'error-one.log', size: '12', modified: 34 }],
        total: '5',
        page: 2,
        page_size: 1,
        total_pages: 5,
        has_more: true,
      })
    ).toEqual({
      files: [{ name: 'error-one.log', size: 12, modified: 34 }],
      total: 5,
      page: 2,
      page_size: 1,
      total_pages: 5,
      has_more: true,
    });
  });

  it('passes pagination and count view when fetching error logs', async () => {
    mocks.get.mockResolvedValue({ files: [], total: 12 });

    await logsApi.fetchErrorLogs({ page: 2, pageSize: 50, view: 'count' });

    expect(mocks.get).toHaveBeenCalledWith('/request-error-logs', {
      params: { page: 2, page_size: 50, search: undefined, view: 'count' },
      timeout: expect.any(Number),
    });
  });
});
