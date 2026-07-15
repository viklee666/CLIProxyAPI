import { useCallback, useEffect, useState } from 'react';
import { useInterval } from '@/hooks/useInterval';
import { apiKeyUsageApi, type ApiKeyUsageItem } from '@/services/api';
import {
  normalizeRecentRequestUsageEntry,
  type RecentRequestUsageEntry,
} from '@/utils/recentRequests';

const PROVIDER_RECENT_REQUESTS_STALE_TIME_MS = 240_000;

export type ProviderRecentRequests = Map<string, Map<string, RecentRequestUsageEntry>>;

export type UseProviderRecentRequestsOptions = {
  enabled?: boolean;
};

const EMPTY_USAGE_BY_PROVIDER: ProviderRecentRequests = new Map();

let cachedUsageByProvider: ProviderRecentRequests = EMPTY_USAGE_BY_PROVIDER;
let cachedAt = 0;
let inFlightRequest: Promise<ProviderRecentRequests> | null = null;

const normalizeProviderKey = (value: unknown): string => String(value ?? '').trim().toLowerCase();

const normalizeApiKeyUsageItems = (items: ApiKeyUsageItem[]): ProviderRecentRequests => {
  if (!Array.isArray(items)) {
    return EMPTY_USAGE_BY_PROVIDER;
  }

  const usageByProvider: ProviderRecentRequests = new Map();
  items.forEach((item) => {
    const providerKey = normalizeProviderKey(item.provider);
    const compositeKey = String(item.composite_key ?? '').trim();
    if (!providerKey || !compositeKey) return;
    let usageByCompositeKey = usageByProvider.get(providerKey);
    if (!usageByCompositeKey) {
      usageByCompositeKey = new Map<string, RecentRequestUsageEntry>();
      usageByProvider.set(providerKey, usageByCompositeKey);
    }
    usageByCompositeKey.set(compositeKey, normalizeRecentRequestUsageEntry(item));
  });

  return usageByProvider;
};

const fetchProviderRecentRequests = async (): Promise<ProviderRecentRequests> => {
  if (!inFlightRequest) {
    inFlightRequest = apiKeyUsageApi
      .getAllUsage()
      .then((items) => {
        const normalized = normalizeApiKeyUsageItems(items);
        cachedUsageByProvider = normalized;
        cachedAt = Date.now();
        return normalized;
      })
      .finally(() => {
        inFlightRequest = null;
      });
  }

  return inFlightRequest;
};

export function useProviderRecentRequests(options: UseProviderRecentRequestsOptions = {}) {
  const enabled = options.enabled ?? true;
  const [usageByProvider, setUsageByProvider] = useState<ProviderRecentRequests>(
    cachedUsageByProvider
  );
  const [isLoading, setIsLoading] = useState(false);

  const loadRecentRequests = useCallback(
    async (loadOptions: { force?: boolean } = {}) => {
      if (!enabled) {
        return EMPTY_USAGE_BY_PROVIDER;
      }

      const hasFreshCache =
        cachedAt > 0 &&
        Date.now() - cachedAt < PROVIDER_RECENT_REQUESTS_STALE_TIME_MS;

      if (!loadOptions.force && hasFreshCache) {
        setUsageByProvider(cachedUsageByProvider);
        return cachedUsageByProvider;
      }

      setIsLoading(true);
      try {
        const nextUsage = await fetchProviderRecentRequests();
        setUsageByProvider(nextUsage);
        return nextUsage;
      } catch {
        if (cachedAt > 0) {
          setUsageByProvider(cachedUsageByProvider);
        }
        return cachedUsageByProvider;
      } finally {
        setIsLoading(false);
      }
    },
    [enabled]
  );

  const refreshRecentRequests = useCallback(
    async () => loadRecentRequests({ force: true }),
    [loadRecentRequests]
  );

  useEffect(() => {
    setUsageByProvider(enabled ? cachedUsageByProvider : EMPTY_USAGE_BY_PROVIDER);
  }, [enabled]);

  useInterval(() => {
    void refreshRecentRequests().catch(() => {});
  }, enabled ? PROVIDER_RECENT_REQUESTS_STALE_TIME_MS : null);

  return {
    usageByProvider: enabled ? usageByProvider : EMPTY_USAGE_BY_PROVIDER,
    isLoading: enabled ? isLoading : false,
    loadRecentRequests,
    refreshRecentRequests,
  };
}
