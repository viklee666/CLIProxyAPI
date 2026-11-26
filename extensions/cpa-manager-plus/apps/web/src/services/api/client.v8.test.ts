import axios, { type AxiosAdapter } from 'axios';
import { describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';

vi.mock('@/features/demo/demoMode', () => ({ DEMO_API_BASE: 'http://demo.local', isDemoMode: () => false }));

describe('v8 management transport', () => {
  it('preserves the configured origin and deployment prefix without duplicating the v0 path', async () => {
    apiClient.setConfig({ apiBase: 'https://manager.example/proxy', managementKey: 'operator-key' });
    let actualURL = '';
    let authorization: unknown;
    let timeout: number | undefined;
    const adapter: AxiosAdapter = async (config) => {
      actualURL = axios.getUri(config);
      authorization = config.headers.Authorization;
      timeout = config.timeout;
      return { data: { answer: 'ok' }, status: 200, statusText: 'OK', headers: {}, config };
    };
    await apiClient.postV8('/v8/management/requests/pelican', { auth_index: 'one' }, { adapter, timeout: 0 });
    expect(actualURL).toBe('https://manager.example/proxy/v8/management/requests/pelican');
    expect(authorization).toBe('Bearer operator-key');
    expect(timeout).toBe(0);
  });

  it('resolves an exact target through the v8 GET endpoint', async () => {
    apiClient.setConfig({ apiBase: 'https://manager.example/proxy', managementKey: 'operator-key' });
    const controller = new AbortController();
    const adapter: AxiosAdapter = async (config) => {
      expect(axios.getUri(config)).toBe('https://manager.example/proxy/v8/management/requests/pelican?provider_kind=openai&config_index=2&key_index=1');
      expect(config.headers.Authorization).toBe('Bearer operator-key');
      expect(config.signal).toBe(controller.signal);
      return { data: { auth_id: 'exact' }, status: 200, statusText: 'OK', headers: {}, config };
    };
    await expect(apiClient.getV8('/v8/management/requests/pelican', {
      params: { provider_kind: 'openai', config_index: 2, key_index: 1 }, signal: controller.signal, adapter,
    })).resolves.toEqual({ auth_id: 'exact' });
  });

  it('rejects arbitrary origins and traversal before attaching management credentials', async () => {
    const adapter = vi.fn();
    for (const path of ['https://attacker.invalid/x', '//attacker.invalid/x', '/v8/management/../other']) {
      await expect(apiClient.postV8(path, {}, { adapter })).rejects.toThrow('Invalid v8');
      await expect(apiClient.getV8(path, { adapter })).rejects.toThrow('Invalid v8');
    }
    expect(adapter).not.toHaveBeenCalled();
  });
});
