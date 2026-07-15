import { apiClient } from './client';
import type { ApiKeyUsageResponse } from '@/utils/recentRequests';

const API_KEY_USAGE_TIMEOUT_MS = 30 * 1000;
const API_KEY_USAGE_PAGE_SIZE = 200;

export type ApiKeyUsageQuery = {
  page?: number;
  pageSize?: number;
  providers?: string[];
  search?: string;
};

export type ApiKeyUsageItem = {
  provider: string;
  composite_key: string;
  success?: unknown;
  failed?: unknown;
  recent_requests?: unknown;
  recentRequests?: unknown;
};

export type ApiKeyUsagePage = {
  items: ApiKeyUsageItem[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  has_more: boolean;
};

const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};

const finiteInteger = (value: unknown, fallback: number): number => {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : fallback;
};

const normalizeItem = (value: unknown): ApiKeyUsageItem | null => {
  const item = asRecord(value);
  const provider = String(item.provider ?? '').trim().toLowerCase();
  const compositeKey = String(item.composite_key ?? item.compositeKey ?? '').trim();
  if (!provider || !compositeKey) return null;

  return {
    provider,
    composite_key: compositeKey,
    success: item.success,
    failed: item.failed,
    recent_requests: item.recent_requests,
    recentRequests: item.recentRequests,
  };
};

const flattenLegacyResponse = (value: ApiKeyUsageResponse): ApiKeyUsageItem[] => {
  const items: ApiKeyUsageItem[] = [];
  Object.entries(value).forEach(([provider, entries]) => {
    if (!entries || typeof entries !== 'object' || Array.isArray(entries)) return;
    Object.entries(entries).forEach(([compositeKey, entry]) => {
      const normalized = normalizeItem({
        ...asRecord(entry),
        provider,
        composite_key: compositeKey,
      });
      if (normalized) items.push(normalized);
    });
  });
  return items;
};

export const normalizeApiKeyUsagePage = (value: unknown): ApiKeyUsagePage => {
  const source = asRecord(value);
  const items = Array.isArray(source.items)
    ? source.items.map(normalizeItem).filter((item): item is ApiKeyUsageItem => item !== null)
    : flattenLegacyResponse(source as ApiKeyUsageResponse);
  const total = Math.max(0, finiteInteger(source.total, items.length));
  const page = Math.max(1, finiteInteger(source.page, 1));
  const pageSize = Math.max(0, finiteInteger(source.page_size ?? source.pageSize, items.length));
  const totalPages = Math.max(
    0,
    finiteInteger(
      source.total_pages ?? source.totalPages,
      pageSize > 0 && total > 0 ? Math.ceil(total / pageSize) : 0
    )
  );
  const hasMoreValue = source.has_more ?? source.hasMore;

  return {
    items,
    total,
    page,
    page_size: pageSize,
    total_pages: totalPages,
    has_more:
      typeof hasMoreValue === 'boolean'
        ? hasMoreValue
        : String(hasMoreValue ?? '').toLowerCase() === 'true' || page < totalPages,
  };
};

const fetchUsagePage = async (query: ApiKeyUsageQuery = {}): Promise<ApiKeyUsagePage> => {
  const data = await apiClient.get('/api-key-usage', {
    params: {
      page: query.page,
      page_size: query.pageSize,
      provider:
        query.providers && query.providers.length > 0
          ? query.providers.map((provider) => provider.trim()).filter(Boolean).join(',') || undefined
          : undefined,
      search: query.search?.trim() || undefined,
    },
    timeout: API_KEY_USAGE_TIMEOUT_MS,
  });
  return normalizeApiKeyUsagePage(data);
};

export const apiKeyUsageApi = {
  getUsage: fetchUsagePage,

  async getAllUsage(
    query: Omit<ApiKeyUsageQuery, 'page' | 'pageSize'> = {}
  ): Promise<ApiKeyUsageItem[]> {
    const firstPage = await fetchUsagePage({ ...query, page: 1, pageSize: API_KEY_USAGE_PAGE_SIZE });
    const items = [...firstPage.items];
    for (let page = 2; page <= firstPage.total_pages; page += 1) {
      const nextPage = await fetchUsagePage({ ...query, page, pageSize: API_KEY_USAGE_PAGE_SIZE });
      items.push(...nextPage.items);
      if (!nextPage.has_more) break;
    }
    return items;
  },
};
