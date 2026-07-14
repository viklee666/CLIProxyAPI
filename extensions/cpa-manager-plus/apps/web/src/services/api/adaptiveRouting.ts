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
};

export const adaptiveRoutingApi = {
  getConfig: () => apiClient.get<AdaptiveRoutingConfig>('/routing/adaptive'),
  updateConfig: (config: AdaptiveRoutingConfig) =>
    apiClient.put('/routing/adaptive', config),
  getScores: (provider = '', model = '') =>
    apiClient.get<AdaptiveRoutingSnapshot>('/routing/adaptive/scores', {
      params: {
        provider: provider.trim() || undefined,
        model: model.trim() || undefined,
      },
    }),
};
