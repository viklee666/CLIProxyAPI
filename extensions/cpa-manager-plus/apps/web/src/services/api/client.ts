/**
 * Axios API client.
 * Replaces the original src/core/api-client.js.
 */

import axios, { AxiosInstance, AxiosRequestConfig, AxiosResponse } from 'axios';
import type { ApiClientConfig, ApiError } from '@/types';
import {
  BUILD_DATE_HEADER_KEYS,
  CPA_SUPPORT_PLUGIN_HEADER_KEYS,
  REQUEST_TIMEOUT_MS,
  VERSION_HEADER_KEYS
} from '@/utils/constants';
import { computeApiUrl } from '@/utils/connection';
import {
  handleDemoApiRequest,
  handleDemoFormRequest,
  handleDemoRawRequest,
} from '@/features/demo/demoApi';
import { isDemoMode } from '@/features/demo/demoMode';

class ApiClient {
  private instance: AxiosInstance;
  private apiBase: string = '';
  private managementKey: string = '';

  constructor() {
    this.instance = axios.create({
      timeout: REQUEST_TIMEOUT_MS,
      headers: {
        'Content-Type': 'application/json'
      }
    });

    this.setupInterceptors();
  }

  /**
   * Configure the management connection.
   */
  setConfig(config: ApiClientConfig): void {
    this.apiBase = computeApiUrl(config.apiBase);
    this.managementKey = config.managementKey;

    if (config.timeout) {
      this.instance.defaults.timeout = config.timeout;
    } else {
      this.instance.defaults.timeout = REQUEST_TIMEOUT_MS;
    }
  }

  private readHeader(
    headers: Record<string, unknown> | undefined,
    keys: string[]
  ): string | null {
    if (!headers) return null;

    const normalizeValue = (value: unknown): string | null => {
      if (value === undefined || value === null) return null;
      if (Array.isArray(value)) {
        const first = value.find((entry) => entry !== undefined && entry !== null && String(entry).trim());
        return first !== undefined ? String(first) : null;
      }
      const text = String(value);
      return text ? text : null;
    };

    const headerGetter = (headers as { get?: (name: string) => unknown }).get;
    if (typeof headerGetter === 'function') {
      for (const key of keys) {
        const match = normalizeValue(headerGetter.call(headers, key));
        if (match) return match;
      }
    }

    const entries =
      typeof (headers as { entries?: () => Iterable<[string, unknown]> }).entries === 'function'
        ? Array.from((headers as { entries: () => Iterable<[string, unknown]> }).entries())
        : Object.entries(headers);

    const normalized = Object.fromEntries(
      entries.map(([key, value]) => [String(key).toLowerCase(), value])
    );
    for (const key of keys) {
      const match = normalizeValue(normalized[key.toLowerCase()]);
      if (match) return match;
    }
    return null;
  }

  private readBooleanHeader(
    headers: Record<string, unknown> | undefined,
    keys: string[]
  ): boolean | null {
    const value = this.readHeader(headers, keys);
    if (value === null) return null;

    const normalized = value.trim().toLowerCase();
    if (['1', 'true', 'yes', 'on'].includes(normalized)) return true;
    if (['0', 'false', 'no', 'off'].includes(normalized)) return false;
    return null;
  }

  /**
   * Install request and response interceptors.
   */
  private setupInterceptors(): void {
    // Request interceptor.
    this.instance.interceptors.request.use(
      (config) => {
        // Set the legacy management base URL.
        config.baseURL = this.apiBase;
        if (config.url) {
          // Normalize deprecated Gemini endpoint to the current path.
          config.url = config.url.replace(/\/generative-language-api-key\b/g, '/gemini-api-key');
        }

        // Attach management authentication.
        if (this.managementKey) {
          config.headers.Authorization = `Bearer ${this.managementKey}`;
        }

        return config;
      },
      (error) => Promise.reject(this.handleError(error))
    );

    // Response interceptor.
    this.instance.interceptors.response.use(
      (response) => {
        const headers = response.headers as Record<string, string | undefined>;
        const version = this.readHeader(headers, VERSION_HEADER_KEYS);
        const buildDate = this.readHeader(headers, BUILD_DATE_HEADER_KEYS);
        const supportsPlugin = this.readBooleanHeader(headers, CPA_SUPPORT_PLUGIN_HEADER_KEYS);

        // Notify stores when server version metadata changes.
        if (version || buildDate) {
          window.dispatchEvent(
            new CustomEvent('server-version-update', {
              detail: { version: version || null, buildDate: buildDate || null }
            })
          );
        }
        if (supportsPlugin !== null) {
          window.dispatchEvent(
            new CustomEvent('server-plugin-support-update', {
              detail: { supportsPlugin }
            })
          );
        }

        return response;
      },
      (error) => Promise.reject(this.handleError(error))
    );
  }

  /**
   * Normalize transport errors.
   */
  private handleError(error: unknown): ApiError {
    const isRecord = (value: unknown): value is Record<string, unknown> =>
      value !== null && typeof value === 'object';

    if (axios.isAxiosError(error)) {
      const responseData: unknown = error.response?.data;
      const responseRecord = isRecord(responseData) ? responseData : null;
      const errorValue = responseRecord?.error;
      const message =
        typeof errorValue === 'string'
          ? errorValue
          : isRecord(errorValue) && typeof errorValue.message === 'string'
            ? errorValue.message
            : typeof responseRecord?.message === 'string'
              ? responseRecord.message
              : error.message || 'Request failed';
      const apiError = new Error(message) as ApiError;
      apiError.name = 'ApiError';
      apiError.status = error.response?.status;
      apiError.code = error.code;
      apiError.details = responseData;
      apiError.data = responseData;

      // Invalid management authentication triggers logout.
      if (error.response?.status === 401) {
        window.dispatchEvent(new Event('unauthorized'));
      }

      return apiError;
    }

    const fallbackMessage =
      error instanceof Error ? error.message : typeof error === 'string' ? error : 'Unknown error occurred';
    const fallback = new Error(fallbackMessage) as ApiError;
    fallback.name = 'ApiError';
    return fallback;
  }

  /**
   * Send a GET request.
   */
  async get<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoApiRequest<T>('get', url, undefined, config);
    }
    const response = await this.instance.get<T>(url, config);
    return response.data;
  }

  /**
   * Send a POST request.
   */
  async post<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoApiRequest<T>('post', url, data, config);
    }
    const response = await this.instance.post<T>(url, data, config);
    return response.data;
  }

  async getV8<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) return handleDemoApiRequest<T>('get', url, undefined, config);
    if (!url.startsWith('/v8/management/') || url.includes('..') || url.includes('\\')) {
      throw new Error('Invalid v8 management path');
    }
    const baseUrl = this.apiBase.replace(/\/v0\/management\/?$/i, '');
    if (!baseUrl) throw new Error('Management connection is not configured');
    const response = await this.instance.get<T>(`${baseUrl}${url}`, config);
    return response.data;
  }

  async postV8<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) return handleDemoApiRequest<T>('post', url, data, config);
    if (!url.startsWith('/v8/management/') || url.includes('..') || url.includes('\\')) {
      throw new Error('Invalid v8 management path');
    }
    const baseUrl = this.apiBase.replace(/\/v0\/management\/?$/i, '');
    if (!baseUrl) throw new Error('Management connection is not configured');
    // An absolute URL retains the configured origin and deployment prefix even
    // when the legacy interceptor supplies its v0 baseURL.
    const response = await this.instance.post<T>(`${baseUrl}${url}`, data, config);
    return response.data;
  }

  /**
   * Send a PUT request.
   */
  async put<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoApiRequest<T>('put', url, data, config);
    }
    const response = await this.instance.put<T>(url, data, config);
    return response.data;
  }

  /**
   * Send a PATCH request.
   */
  async patch<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoApiRequest<T>('patch', url, data, config);
    }
    const response = await this.instance.patch<T>(url, data, config);
    return response.data;
  }

  /**
   * Send a DELETE request.
   */
  async delete<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoApiRequest<T>('delete', url, undefined, config);
    }
    const response = await this.instance.delete<T>(url, config);
    return response.data;
  }

  /**
   * Return the raw response for downloads.
   */
  async getRaw(url: string, config?: AxiosRequestConfig): Promise<AxiosResponse> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoRawRequest(url, config);
    }
    return this.instance.get(url, config);
  }

  /**
   * Send multipart form data.
   */
  async postForm<T = unknown>(
    url: string,
    formData: FormData,
    config?: AxiosRequestConfig
  ): Promise<T> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoFormRequest<T>(url, formData, config);
    }
    const response = await this.instance.post<T>(url, formData, {
      ...config,
      headers: {
        ...(config?.headers || {}),
        'Content-Type': 'multipart/form-data'
      }
    });
    return response.data;
  }

  /**
   * Expose raw requests for downloads and custom transports.
   */
  async requestRaw(config: AxiosRequestConfig): Promise<AxiosResponse> {
    if (__DEMO_SITE__ && isDemoMode()) {
      return handleDemoRawRequest(config.url || '/', config);
    }
    return this.instance.request(config);
  }
}

// Shared API client.
export const apiClient = new ApiClient();
