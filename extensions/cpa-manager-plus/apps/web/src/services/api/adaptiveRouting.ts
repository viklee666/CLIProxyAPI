import { apiClient } from './client';

export type AdaptiveRoutingConfig = {
  'top-k': number;
  'ewma-alpha': number;
  'ttft-target-ms': number;
  weights: {
    priority: number;
    load: number;
    'success-rate': number;
    ttft: number;
  };
  'sticky-escape': {
    enabled: boolean;
    'min-samples': number;
    'error-rate-threshold': number;
    'ttft-threshold-ms': number;
    'active-request-threshold': number;
  };
};

export type AdaptiveRoutingScore = {
  auth_id: string;
  auth_index: string;
  provider: string;
  priority: number;
  active_requests: number;
  success_rate: number;
  error_rate: number;
  ttft_ms: number;
  outcome_samples: number;
  ttft_samples: number;
  score: number;
  rank: number;
  eligible: boolean;
  escape_sticky: boolean;
  last_sample_at?: string;
};

export type AdaptiveRoutingSnapshot = {
  enabled: boolean;
  config: AdaptiveRoutingConfig;
  items: AdaptiveRoutingScore[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  has_more: boolean;
};

export type AdaptiveRoutingScoresQuery = {
  provider?: string;
  model?: string;
  search?: string;
  eligible?: boolean;
  page?: number;
  pageSize?: number;
};

const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};

const finiteInteger = (value: unknown, fallback: number): number => {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : fallback;
};

export const normalizeAdaptiveRoutingSnapshot = (value: unknown): AdaptiveRoutingSnapshot => {
  const source = asRecord(value);
  const items = Array.isArray(source.items) ? (source.items as AdaptiveRoutingScore[]) : [];
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
    enabled: source.enabled === true,
    config: asRecord(source.config) as AdaptiveRoutingConfig,
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

export const adaptiveRoutingApi = {
  getConfig: () => apiClient.get<AdaptiveRoutingConfig>('/routing/adaptive'),
  updateConfig: (config: AdaptiveRoutingConfig) =>
    apiClient.put('/routing/adaptive', config),
  async getScores(query: AdaptiveRoutingScoresQuery = {}): Promise<AdaptiveRoutingSnapshot> {
    const data = await apiClient.get('/routing/adaptive/scores', {
      params: {
        provider: query.provider?.trim() || undefined,
        model: query.model?.trim() || undefined,
        search: query.search?.trim() || undefined,
        eligible: query.eligible,
        page: query.page,
        page_size: query.pageSize,
      },
    });
    return normalizeAdaptiveRoutingSnapshot(data);
  },
};
