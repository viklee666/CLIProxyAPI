import { apiClient } from './client';

export type ClientAccessPage<T> = {
  items: T[];
  total: number;
  page: number;
  page_size: number;
};

export type ClientGroup = {
  id: number;
  name: string;
  description?: string;
  enabled: boolean;
  key_count: number;
  credential_count: number;
  created_at: string;
  updated_at: string;
};

export type ClientKey = {
  id: number;
  name: string;
  key_prefix: string;
  key_mask: string;
  enabled: boolean;
  allow_all_groups: boolean;
  allow_ungrouped: boolean;
  group_ids: number[];
  expires_at?: string;
  rpm_limit: number;
  concurrency_limit: number;
  current_concurrency: number;
  request_limit_total: number;
  request_used_total: number;
  request_limit_5h: number;
  request_used_5h: number;
  request_limit_1d: number;
  request_used_1d: number;
  request_limit_7d: number;
  request_used_7d: number;
  token_limit_total: number;
  token_used_total: number;
  token_limit_5h: number;
  token_used_5h: number;
  token_limit_1d: number;
  token_used_1d: number;
  token_limit_7d: number;
  token_used_7d: number;
  last_used_at?: string;
  created_at: string;
  updated_at: string;
};

export type CreatedClientKey = ClientKey & { secret: string };

export type CredentialBinding = {
  auth_index: string;
  group_id: number;
  priority: number;
  created_at: string;
};

export type ClientGroupInput = {
  name: string;
  description: string;
  enabled?: boolean;
};

export type ClientKeyInput = {
  name: string;
  secret?: string;
  enabled?: boolean;
  allow_all_groups?: boolean;
  allow_ungrouped?: boolean;
  group_ids?: number[];
  expires_at?: string | null;
  clear_expires_at?: boolean;
  rpm_limit?: number;
  concurrency_limit?: number;
  request_limit_total?: number;
  request_limit_5h?: number;
  request_limit_1d?: number;
  request_limit_7d?: number;
  token_limit_total?: number;
  token_limit_5h?: number;
  token_limit_1d?: number;
  token_limit_7d?: number;
  reset_request_usage?: boolean;
  reset_token_usage?: boolean;
};

const listQuery = (page: number, pageSize: number, search: string) => ({
  params: { page, page_size: pageSize, search: search.trim() || undefined },
});

export const clientAccessApi = {
  listGroups: (page = 1, pageSize = 50, search = '') =>
    apiClient.get<ClientAccessPage<ClientGroup>>(
      '/client-access/groups',
      listQuery(page, pageSize, search)
    ),
  createGroup: (input: ClientGroupInput) =>
    apiClient.post<ClientGroup>('/client-access/groups', input),
  updateGroup: (id: number, input: Partial<ClientGroupInput>) =>
    apiClient.patch<ClientGroup>(`/client-access/groups/${id}`, input),
  deleteGroup: (id: number) => apiClient.delete(`/client-access/groups/${id}`),

  listKeys: (page = 1, pageSize = 20, search = '') =>
    apiClient.get<ClientAccessPage<ClientKey>>(
      '/client-access/keys',
      listQuery(page, pageSize, search)
    ),
  createKey: (input: ClientKeyInput) =>
    apiClient.post<CreatedClientKey>('/client-access/keys', input),
  updateKey: (id: number, input: Partial<ClientKeyInput>) =>
    apiClient.patch<ClientKey>(`/client-access/keys/${id}`, input),
  deleteKey: (id: number) => apiClient.delete(`/client-access/keys/${id}`),

  listCredentialBindings: (
    page = 1,
    pageSize = 200,
    search = '',
    authIndices: string[] = []
  ) =>
    apiClient.get<ClientAccessPage<CredentialBinding>>(
      '/client-access/credential-bindings',
      {
        params: {
          ...listQuery(page, pageSize, search).params,
          auth_indices: authIndices.length > 0 ? authIndices.join(',') : undefined,
        },
      }
    ),
  replaceCredentialBindings: (
    authIndices: string[],
    groups: Array<{ group_id: number; priority: number }>
  ) =>
    apiClient.put<{ updated: number }>('/client-access/credential-bindings', {
      auth_indices: authIndices,
      groups,
    }),
};
