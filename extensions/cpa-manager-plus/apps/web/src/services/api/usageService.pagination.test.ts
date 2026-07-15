import axios from 'axios';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { usageServiceApi } from './usageService';

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('usageServiceApi bounded Manager lists', () => {
  it('merges bounded model-price and API-key-alias pages', async () => {
    const get = vi
      .spyOn(axios, 'get')
      .mockResolvedValueOnce({
        data: {
          prices: { 'model-a': { prompt: 1, completion: 2, cache: 0 } },
          page: 1,
          page_size: 1,
          total_pages: 2,
          has_more: true,
        },
      })
      .mockResolvedValueOnce({
        data: {
          prices: { 'model-b': { prompt: 3, completion: 4, cache: 0 } },
          page: 2,
          page_size: 1,
          total_pages: 2,
          has_more: false,
        },
      })
      .mockResolvedValueOnce({
        data: {
          items: [{ apiKeyHash: 'a'.repeat(64), alias: 'Alpha' }],
          page: 1,
          page_size: 1,
          total_pages: 2,
          has_more: true,
        },
      })
      .mockResolvedValueOnce({
        data: {
          items: [{ apiKeyHash: 'b'.repeat(64), alias: 'Beta' }],
          page: 2,
          page_size: 1,
          total_pages: 2,
          has_more: false,
        },
      });

    const prices = await usageServiceApi.getModelPrices('http://manager.local', 'key');
    expect(Object.keys(prices.prices)).toEqual(['model-a', 'model-b']);
    const aliases = await usageServiceApi.getApiKeyAliases('http://manager.local', 'key');
    expect(aliases.items.map((item) => item.alias)).toEqual(['Alpha', 'Beta']);
    expect(get).toHaveBeenCalledTimes(4);
    expect(get.mock.calls[1]?.[1]?.params).toEqual({ page: 2, page_size: 1 });
    expect(get.mock.calls[3]?.[1]?.params).toEqual({ page: 2, page_size: 1 });
  });

  it('reloads model prices through bounded GET pages after sync', async () => {
    const post = vi.spyOn(axios, 'post').mockResolvedValue({
      data: { imported: 1, skipped: 0, matched: { 'model-a': { prompt: 1 } } },
    });
    const get = vi.spyOn(axios, 'get').mockResolvedValue({
      data: {
        prices: { 'model-a': { prompt: 1, completion: 2, cache: 0 } },
        page: 1,
        page_size: 100,
        total_pages: 1,
        has_more: false,
      },
    });

    const result = await usageServiceApi.syncModelPrices('http://manager.local', 'key', [
      'model-a',
    ]);

    expect(post).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledTimes(1);
    expect(result.prices).toEqual({
      'model-a': { prompt: 1, completion: 2, cache: 0 },
    });
  });

  it('loads quota cooldowns through bounded pages', async () => {
    const get = vi
      .spyOn(axios, 'get')
      .mockResolvedValueOnce({
        data: {
          items: [{ authFileName: 'a.json', recoverAtMs: 1 }],
          page: 1,
          page_size: 200,
          total: 2,
          total_pages: 2,
          has_more: true,
        },
      })
      .mockResolvedValueOnce({
        data: {
          items: [{ authFileName: 'b.json', recoverAtMs: 2 }],
          page: 2,
          page_size: 200,
          total: 2,
          total_pages: 2,
          has_more: false,
        },
      });

    await expect(
      usageServiceApi.getActiveQuotaCooldowns('http://manager.local', 'key', {
        provider: 'codex',
      })
    ).resolves.toEqual([
      { authFileName: 'a.json', recoverAtMs: 1 },
      { authFileName: 'b.json', recoverAtMs: 2 },
    ]);
    expect(get).toHaveBeenCalledTimes(2);
    expect(get.mock.calls[0]?.[1]?.params).toEqual(
      expect.objectContaining({ provider: 'codex', page: 1, page_size: 200 })
    );
    expect(get.mock.calls[1]?.[1]?.params).toEqual(
      expect.objectContaining({ provider: 'codex', page: 2, page_size: 200 })
    );
  });

  it('hydrates Codex results and logs with independent projected pages', async () => {
    const get = vi
      .spyOn(axios, 'get')
      .mockResolvedValueOnce({
        data: {
          run: { id: 9 },
          results: [{ id: 1 }],
          logs: [{ id: 11 }],
          results_pagination: {
            page: 1,
            page_size: 1,
            total: 2,
            total_pages: 2,
            has_more: true,
          },
          logs_pagination: {
            page: 1,
            page_size: 1,
            total: 2,
            total_pages: 2,
            has_more: true,
          },
        },
      })
      .mockResolvedValueOnce({ data: { run: { id: 9 }, results: [{ id: 2 }], logs: [] } })
      .mockResolvedValueOnce({ data: { run: { id: 9 }, results: [], logs: [{ id: 12 }] } });

    const detail = await usageServiceApi.getCodexInspectionRun('http://manager.local', 'key', 9);

    expect(detail.results.map((item) => item.id)).toEqual([1, 2]);
    expect(detail.logs.map((item) => item.id)).toEqual([11, 12]);
    expect(get).toHaveBeenCalledTimes(3);
    expect(get.mock.calls[1]?.[1]?.params).toEqual({
      include_results: true,
      include_logs: false,
      results_page: 2,
      results_page_size: 1,
    });
    expect(get.mock.calls[2]?.[1]?.params).toEqual({
      include_results: false,
      include_logs: true,
      logs_page: 2,
      logs_page_size: 1,
    });
  });

  it('requests the run list with the new page contract', async () => {
    const get = vi.spyOn(axios, 'get').mockResolvedValue({ data: { items: [] } });

    await usageServiceApi.listCodexInspectionRuns('http://manager.local', 'key', 30);

    expect(get.mock.calls[0]?.[1]?.params).toEqual({ page: 1, page_size: 30 });
  });

  it('chunks Codex action IDs to the server hard limit', async () => {
    const post = vi
      .spyOn(axios, 'post')
      .mockResolvedValueOnce({
        data: {
          outcomes: [{ resultId: 1, success: true }],
          detail: { run: { id: 9 }, results: [], logs: [] },
        },
      })
      .mockResolvedValueOnce({
        data: {
          outcomes: [{ resultId: 201, success: true }],
          detail: { run: { id: 9 }, results: [], logs: [] },
        },
      });
    const ids = Array.from({ length: 201 }, (_, index) => index + 1);

    const response = await usageServiceApi.executeCodexInspectionActions(
      'http://manager.local',
      'key',
      9,
      ids
    );

    expect(post).toHaveBeenCalledTimes(2);
    expect(post.mock.calls[0]?.[1]).toEqual({ resultIds: ids.slice(0, 200) });
    expect(post.mock.calls[1]?.[1]).toEqual({ resultIds: [201] });
    expect(response.outcomes.map((item) => item.resultId)).toEqual([1, 201]);
  });
});
