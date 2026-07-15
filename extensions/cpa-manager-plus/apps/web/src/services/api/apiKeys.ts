/**
 * API 密钥管理
 */

import { apiClient } from './client';

const extractKeys = (data: unknown): string[] => {
  if (Array.isArray(data)) return data.map((key) => String(key));
  if (data === null || typeof data !== 'object') return [];
  const record = data as Record<string, unknown>;
  const keys = record['api-keys'] ?? record.apiKeys ?? record.items;
  return Array.isArray(keys) ? keys.map((key) => String(key)) : [];
};

const positiveInteger = (value: unknown): number | null => {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
};

export const apiKeysApi = {
  async list(): Promise<string[]> {
    let data = await apiClient.get<unknown>('/api-keys');
    const keys = extractKeys(data);
    if (data === null || typeof data !== 'object' || Array.isArray(data)) return keys;

    let record = data as Record<string, unknown>;
    let page = positiveInteger(record.page) ?? 1;
    const pageSize = positiveInteger(record.page_size) ?? 100;
    const totalPages = positiveInteger(record.total_pages);
    while (record.has_more === true && (!totalPages || page < totalPages)) {
      page += 1;
      data = await apiClient.get<unknown>('/api-keys', { params: { page, page_size: pageSize } });
      keys.push(...extractKeys(data));
      if (data === null || typeof data !== 'object' || Array.isArray(data)) break;
      record = data as Record<string, unknown>;
    }
    return keys;
  },

  async first(): Promise<string | null> {
    const data = await apiClient.get<unknown>('/api-keys', { params: { page: 1, page_size: 1 } });
    return extractKeys(data)[0] ?? null;
  },

  create: (value: string) => apiClient.post('/api-keys', { value }),

  replace: (keys: string[]) => apiClient.put('/api-keys', keys),

  update: (index: number, value: string) => apiClient.patch('/api-keys', { index, value }),

  delete: (index: number) => apiClient.delete(`/api-keys?index=${index}`),
};
