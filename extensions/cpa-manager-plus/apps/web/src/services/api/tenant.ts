import axios, { type AxiosRequestConfig } from 'axios';
import type {
  ClientAccessPage,
  ClientGroup,
  ClientGroupInput,
  ClientKey,
  ClientKeyInput,
  CreatedClientKey,
  CredentialBinding,
} from './clientAccess';
import { detectApiBaseFromLocation, normalizeApiBase } from '@/utils/connection';

export type TenantProfile = {
  id: number;
  display_name: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

export type TenantProviderChannel =
  | 'claude'
  | 'codex'
  | 'gemini'
  | 'xai'
  | 'openai-compat'
  | 'vertex';

export type TenantProvider = {
  id: number;
  tenant_id: number;
  channel: TenantProviderChannel;
  name: string;
  base_url: string;
  proxy_url: string;
  priority: number;
  prefix: string;
  disabled: boolean;
  headers: Record<string, string>;
  models: unknown;
  extra: unknown;
  auth_index: string;
  created_at: string;
  updated_at: string;
};

export type TenantProviderInput = {
  channel: TenantProviderChannel;
  name: string;
  base_url?: string;
  api_key?: string;
  proxy_url?: string;
  priority?: number;
  prefix?: string;
  disabled?: boolean;
  headers?: Record<string, string>;
  models?: unknown;
  extra?: unknown;
};

export type TenantProviderTestResult = {
  status_code: number;
  body: string;
};

export type TenantDiscoveredModel = {
  name: string;
  alias?: string;
  description?: string;
};

export type TenantProviderModelsResult = {
  models: TenantDiscoveredModel[];
};

export type TenantLoginResponse = {
  token: string;
  tenant: TenantProfile;
  expires_at: string;
};

export type TenantUsagePage = {
  items: Array<Record<string, unknown>>;
  total: number;
  page: number;
  page_size: number;
  total_pages?: number;
  has_more?: boolean;
  next_cursor?: string;
};

const normalizeTenantApiBase = (value: string) =>
  normalizeApiBase(value || detectApiBaseFromLocation()).replace(/\/v0\/tenant\/?$/i, '');

const tenantURL = (base: string, path: string) =>
  `${normalizeTenantApiBase(base)}/v0/tenant${path.startsWith('/') ? path : `/${path}`}`;

class TenantApiClient {
  private apiBase = '';
  private token = '';

  configure(apiBase: string, token: string) {
    this.apiBase = normalizeTenantApiBase(apiBase);
    this.token = token.trim();
  }

  clear() {
    this.apiBase = '';
    this.token = '';
  }

  private requestConfig(config?: AxiosRequestConfig): AxiosRequestConfig {
    return {
      ...config,
      headers: {
        ...(config?.headers ?? {}),
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
      },
    };
  }

  private async execute<T>(request: Promise<{ data: T }>): Promise<T> {
    try {
      return (await request).data;
    } catch (error) {
      if (
        axios.isAxiosError(error) &&
        error.response?.status === 401 &&
        typeof window !== 'undefined'
      ) {
        window.dispatchEvent(new Event('tenant-unauthorized'));
      }
      throw error;
    }
  }

  get<T>(path: string, config?: AxiosRequestConfig) {
    return this.execute(axios.get<T>(tenantURL(this.apiBase, path), this.requestConfig(config)));
  }

  post<T>(path: string, body?: unknown, config?: AxiosRequestConfig) {
    return this.execute(
      axios.post<T>(tenantURL(this.apiBase, path), body, this.requestConfig(config))
    );
  }

  patch<T>(path: string, body?: unknown, config?: AxiosRequestConfig) {
    return this.execute(
      axios.patch<T>(tenantURL(this.apiBase, path), body, this.requestConfig(config))
    );
  }

  put<T>(path: string, body?: unknown, config?: AxiosRequestConfig) {
    return this.execute(
      axios.put<T>(tenantURL(this.apiBase, path), body, this.requestConfig(config))
    );
  }

  delete<T>(path: string, config?: AxiosRequestConfig) {
    return this.execute(axios.delete<T>(tenantURL(this.apiBase, path), this.requestConfig(config)));
  }
}

export const tenantApiClient = new TenantApiClient();

export const tenantAuthApi = {
  login: async (apiBase: string, password: string): Promise<TenantLoginResponse> => {
    const response = await axios.post<TenantLoginResponse>(tenantURL(apiBase, '/login'), {
      password,
    });
    return response.data;
  },
  me: () => tenantApiClient.get<TenantProfile>('/me'),
  logout: () => tenantApiClient.post<void>('/logout'),
  changePassword: (currentPassword: string, newPassword: string) =>
    tenantApiClient.post<void>('/change-password', {
      current_password: currentPassword,
      new_password: newPassword,
    }),
};

export const tenantProvidersApi = {
  list: () => tenantApiClient.get<TenantProvider[]>('/providers'),
  create: (input: TenantProviderInput) => tenantApiClient.post<TenantProvider>('/providers', input),
  update: (id: number, input: Partial<TenantProviderInput>) =>
    tenantApiClient.patch<TenantProvider>(`/providers/${id}`, input),
  delete: (id: number) => tenantApiClient.delete<void>(`/providers/${id}`),
  test: (id: number) => tenantApiClient.post<TenantProviderTestResult>(`/providers/${id}/test`),
  models: (id: number) => tenantApiClient.post<TenantProviderModelsResult>(`/providers/${id}/models`),
};

const listQuery = (page: number, pageSize: number, search: string) => ({
  params: { page, page_size: pageSize, search: search.trim() || undefined },
});

export const tenantClientAccessApi = {
  listGroups: (page = 1, pageSize = 50, search = '') =>
    tenantApiClient.get<ClientAccessPage<ClientGroup>>(
      '/groups',
      listQuery(page, pageSize, search)
    ),
  createGroup: (input: ClientGroupInput) => tenantApiClient.post<ClientGroup>('/groups', input),
  updateGroup: (id: number, input: Partial<ClientGroupInput>) =>
    tenantApiClient.patch<ClientGroup>(`/groups/${id}`, input),
  deleteGroup: (id: number) => tenantApiClient.delete<void>(`/groups/${id}`),
  listBindings: (page = 1, pageSize = 500, groupIDs: number[] = []) =>
    tenantApiClient.get<ClientAccessPage<CredentialBinding>>('/credential-bindings', {
      params: {
        page,
        page_size: pageSize,
        group_ids: groupIDs.length > 0 ? groupIDs.join(',') : undefined,
      },
    }),
  replaceGroupBindings: (groupID: number, authIndices: string[], priority: number) =>
    tenantApiClient.put<{ updated: number }>(`/groups/${groupID}/credential-bindings`, {
      auth_indices: authIndices,
      priority,
    }),
  listKeys: (page = 1, pageSize = 50, search = '') =>
    tenantApiClient.get<ClientAccessPage<ClientKey>>('/keys', listQuery(page, pageSize, search)),
  createKey: (input: ClientKeyInput) => tenantApiClient.post<CreatedClientKey>('/keys', input),
  updateKey: (id: number, input: Partial<ClientKeyInput>) =>
    tenantApiClient.patch<ClientKey>(`/keys/${id}`, input),
  deleteKey: (id: number) => tenantApiClient.delete<void>(`/keys/${id}`),
};

export const tenantObservabilityApi = {
  dashboard: (todayStartMS: number, nowMS = Date.now()) =>
    tenantApiClient.get<Record<string, unknown>>('/dashboard/summary', {
      params: { today_start_ms: todayStartMS, now_ms: nowMS },
    }),
  analytics: (fromMS: number, toMS: number) =>
    tenantApiClient.post<Record<string, unknown>>('/monitoring/analytics', {
      from_ms: fromMS,
      to_ms: toMS,
      now_ms: toMS,
      filters: { include_failed: true },
      include: {
        summary: true,
        summary_profile: 'compact',
        timeline: true,
        model_share: true,
        channel_share: true,
        recent_failures: 20,
        granularity: 'hour',
      },
    }),
  usage: (page = 1, pageSize = 50) =>
    tenantApiClient.get<TenantUsagePage>('/usage', { params: { page, page_size: pageSize } }),
};
