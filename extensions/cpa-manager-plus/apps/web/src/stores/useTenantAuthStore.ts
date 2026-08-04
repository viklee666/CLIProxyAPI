import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { tenantApiClient, tenantAuthApi, type TenantProfile } from '@/services/api/tenant';
import { obfuscatedStorage } from '@/services/storage/secureStorage';
import { STORAGE_KEY_TENANT_AUTH, STORAGE_KEY_TENANT_LOGGED_IN } from '@/utils/constants';
import { detectApiBaseFromLocation, normalizeApiBase } from '@/utils/connection';

type TenantConnectionStatus = 'connected' | 'disconnected' | 'connecting' | 'error';

type TenantAuthState = {
  isAuthenticated: boolean;
  apiBase: string;
  token: string;
  tenant: TenantProfile | null;
  expiresAt: string | null;
  rememberSession: boolean;
  connectionStatus: TenantConnectionStatus;
  connectionError: string | null;
  login: (credentials: {
    apiBase: string;
    password: string;
    rememberSession: boolean;
  }) => Promise<void>;
  restoreSession: () => Promise<boolean>;
  checkAuth: () => Promise<boolean>;
  logout: () => Promise<void>;
  clearSession: () => void;
};

const normalizeTenantBase = (value: string) =>
  normalizeApiBase(value || detectApiBaseFromLocation()).replace(/\/v0\/tenant\/?$/i, '');

const clearPersistedSession = () => {
  localStorage.removeItem(STORAGE_KEY_TENANT_LOGGED_IN);
};

export const useTenantAuthStore = create<TenantAuthState>()(
  persist(
    (set, get) => {
      const clearSession = () => {
        tenantApiClient.clear();
        clearPersistedSession();
        set({
          isAuthenticated: false,
          apiBase: '',
          token: '',
          tenant: null,
          expiresAt: null,
          connectionStatus: 'disconnected',
          connectionError: null,
        });
      };

      return {
        isAuthenticated: false,
        apiBase: '',
        token: '',
        tenant: null,
        expiresAt: null,
        rememberSession: false,
        connectionStatus: 'disconnected',
        connectionError: null,
        clearSession,

        login: async ({ apiBase, password, rememberSession }) => {
          const normalizedBase = normalizeTenantBase(apiBase);
          set({ connectionStatus: 'connecting', connectionError: null });
          try {
            const response = await tenantAuthApi.login(normalizedBase, password.trim());
            tenantApiClient.configure(normalizedBase, response.token);
            set({
              isAuthenticated: true,
              apiBase: normalizedBase,
              token: response.token,
              tenant: response.tenant,
              expiresAt: response.expires_at,
              rememberSession,
              connectionStatus: 'connected',
              connectionError: null,
            });
            if (rememberSession) {
              localStorage.setItem(STORAGE_KEY_TENANT_LOGGED_IN, 'true');
            } else {
              clearPersistedSession();
            }
          } catch (error) {
            const message = error instanceof Error ? error.message : 'Tenant login failed';
            tenantApiClient.clear();
            set({
              isAuthenticated: false,
              token: '',
              tenant: null,
              connectionStatus: 'error',
              connectionError: message,
            });
            throw error;
          }
        },

        checkAuth: async () => {
          const { apiBase, token } = get();
          if (!apiBase || !token) {
            return false;
          }
          tenantApiClient.configure(apiBase, token);
          try {
            const tenant = await tenantAuthApi.me();
            set({
              isAuthenticated: true,
              tenant,
              connectionStatus: 'connected',
              connectionError: null,
            });
            return true;
          } catch {
            clearSession();
            return false;
          }
        },

        restoreSession: async () => {
          const { apiBase, token } = get();
          if (localStorage.getItem(STORAGE_KEY_TENANT_LOGGED_IN) !== 'true' || !apiBase || !token) {
            return false;
          }
          return get().checkAuth();
        },

        logout: async () => {
          const { token } = get();
          try {
            if (token) {
              await tenantAuthApi.logout();
            }
          } catch {
            // The server may already have revoked this session. Local teardown
            // is still required so the next request cannot reuse the token.
          } finally {
            clearSession();
          }
        },
      };
    },
    {
      name: STORAGE_KEY_TENANT_AUTH,
      storage: createJSONStorage(() => ({
        getItem: (name) => {
          const data = obfuscatedStorage.getItem<TenantAuthState>(name);
          return data ? JSON.stringify(data) : null;
        },
        setItem: (name, value) => {
          obfuscatedStorage.setItem(name, JSON.parse(value));
        },
        removeItem: (name) => {
          obfuscatedStorage.removeItem(name);
        },
      })),
      partialize: (state) => ({
        apiBase: state.apiBase,
        ...(state.rememberSession
          ? { token: state.token, tenant: state.tenant, expiresAt: state.expiresAt }
          : {}),
        rememberSession: state.rememberSession,
      }),
    }
  )
);

if (typeof window !== 'undefined') {
  window.addEventListener('tenant-unauthorized', () => {
    useTenantAuthStore.getState().clearSession();
  });
}
