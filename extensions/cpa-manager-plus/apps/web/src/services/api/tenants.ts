import { apiClient } from './client';
import type { TenantProfile } from './tenant';

export type TenantProviderAdminView = {
  id: number;
  tenant_id: number;
  tenant_display_name: string;
  channel: string;
  name: string;
  base_url: string;
  api_key: string;
  proxy_url: string;
  priority: number;
  disabled: boolean;
  auth_index: string;
  model_count: number;
  created_at: string;
  updated_at: string;
};

export const tenantsApi = {
  list: () => apiClient.get<TenantProfile[]>('/tenants'),
  create: (input: { display_name: string; password?: string }) =>
    apiClient.post<{ tenant: TenantProfile; password: string }>('/tenants', input),
  update: (id: number, input: { display_name?: string; enabled?: boolean }) =>
    apiClient.patch<TenantProfile>(`/tenants/${id}`, input),
  resetPassword: (id: number) => apiClient.post<{ password: string }>(`/tenants/${id}/reset-password`),
  delete: (id: number) => apiClient.delete<void>(`/tenants/${id}`),
  listProviders: (tenantID: number) =>
    apiClient.get<TenantProviderAdminView[]>(`/tenants/${tenantID}/providers`),
  listAllProviders: () => apiClient.get<TenantProviderAdminView[]>('/tenant-providers'),
};
