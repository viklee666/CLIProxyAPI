import { apiClient } from '@/services/api/client';
import type { Config } from '@/types/config';
import { extractArrayPayload } from '../model/base';
import { normalizeOpenAIChannel } from '../model/authMeta';
import type { MonitoringChannelMeta, MonitoringMetaPayload } from '../model/types';

export const loadMonitoringMetaPayload = async (
  config: Config | null | undefined
): Promise<MonitoringMetaPayload> => {
  let channels: MonitoringChannelMeta[] = [];
  let error = '';

  try {
    const response = await apiClient.get('/openai-compatibility');
    channels = extractArrayPayload(response, 'openai-compatibility')
      .map((item, index) => normalizeOpenAIChannel(item, index))
      .filter(Boolean) as MonitoringChannelMeta[];
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  }

  if (channels.length === 0 && config?.openaiCompatibility?.length) {
    channels = config.openaiCompatibility
      .map((item, index) =>
        normalizeOpenAIChannel(
          {
            ...item,
            'base-url': item.baseUrl,
            'api-key-entries': item.apiKeyEntries,
            models: item.models,
          },
          index
        )
      )
      .filter(Boolean) as MonitoringChannelMeta[];
  }

  return { authFiles: [], channels, error };
};
