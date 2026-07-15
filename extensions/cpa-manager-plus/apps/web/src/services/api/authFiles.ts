/**
 * 认证文件与 OAuth 排除模型相关 API
 */

import { apiClient } from './client';
import type { AuthFilesResponse } from '@/types/authFile';
import type { OAuthModelAliasEntry } from '@/types';
import { parseTimestampMs } from '@/utils/timestamp';
import { useCredentialStatusStore } from '@/stores/useCredentialStatusStore';

type StatusError = { status?: number };
type AuthFileStatusResponse = { status: string; disabled: boolean };
export type AuthFileStatusTarget = {
  name: string;
  authIndex?: AuthFilePatchAuthIndex | null;
};
type AuthFileBatchStatusItem = {
  name?: unknown;
  auth_index?: unknown;
  disabled?: unknown;
};
type AuthFileBatchStatusFailure = {
  name?: unknown;
  auth_index?: unknown;
  error?: unknown;
};
type AuthFileBatchStatusResponse = {
  status?: unknown;
  updated?: unknown;
  items?: unknown;
  failed?: unknown;
};
export type AuthFileBatchStatusResult = {
  status: string;
  updated: number;
  items: Array<{ name: string; authIndex: string; disabled: boolean }>;
  failed: AuthFileBatchFailure[];
};
export type AuthFileFieldsTarget = {
  name: string;
  authIndexes?: AuthFilePatchAuthIndex[];
};
type AuthFileBatchFieldsResponse = {
  status?: unknown;
  updated?: unknown;
  items?: unknown;
  failed?: unknown;
};
export type AuthFileBatchFieldsResult = {
  status: string;
  updated: number;
  failed: AuthFileBatchFailure[];
};
export type AuthFileBatchDownloadResult = {
  blob: Blob;
  filename: string;
  included: number;
  failed: number;
};
type AuthFilePatchPayload = { name: string; disabled?: boolean; [key: string]: unknown };
type AuthFileEntry = AuthFilesResponse['files'][number];
export type AuthFilesListQuery = {
  view?: 'detail' | 'summary' | 'snapshot' | 'count';
  page?: number;
  page_size?: number;
  search?: string;
  provider?: string;
  plan_type?: string;
  auth_index?: string;
  group_id?: number;
  disabled?: boolean;
  problem?: boolean;
  healthy?: boolean;
  sort?: 'provider' | 'name' | 'note' | 'priority' | 'status' | 'updated_at';
  order?: 'asc' | 'desc';
  updated_after_ms?: number;
};
type AuthFileJsonValue = Record<string, unknown> | Record<string, unknown>[];
export type AuthFileFieldsPatch = {
  prefix?: string;
  proxy_url?: string;
  websockets?: boolean;
  using_api?: boolean;
  headers?: Record<string, string>;
  priority?: number;
  note?: string;
};
export type AuthFilePatchAuthIndex = string | number;
type AuthFileBatchFailure = { name: string; error: string };
type AuthFileBatchUploadResponse = {
  status?: string;
  uploaded?: number;
  files?: unknown;
  failed?: unknown;
};
type AuthFileBatchDeleteResponse = {
  status?: string;
  deleted?: number;
  files?: unknown;
  failed?: unknown;
};
type AuthFileBatchUploadResult = {
  status: string;
  uploaded: number;
  files: string[];
  failed: AuthFileBatchFailure[];
};
type AuthFileBatchDeleteResult = {
  status: string;
  deleted: number;
  files: string[];
  failed: AuthFileBatchFailure[];
};

export const AUTH_FILE_INVALID_JSON_OBJECT_ERROR = 'AUTH_FILE_INVALID_JSON_OBJECT';

const getStatusCode = (err: unknown): number | undefined => {
  if (!err || typeof err !== 'object') return undefined;
  if ('status' in err) return (err as StatusError).status;
  return undefined;
};

const normalizeRequestedAuthFileNames = (names: string[]): string[] => {
  const seen = new Set<string>();
  const normalized: string[] = [];

  names.forEach((name) => {
    const trimmed = String(name ?? '').trim();
    if (!trimmed || seen.has(trimmed)) return;
    seen.add(trimmed);
    normalized.push(trimmed);
  });

  return normalized;
};

const normalizeBatchFileNames = (value: unknown): string[] => {
  if (!Array.isArray(value)) return [];
  return normalizeRequestedAuthFileNames(value.map((item) => String(item ?? '')));
};

const normalizeBatchFailures = (value: unknown): AuthFileBatchFailure[] => {
  if (!Array.isArray(value)) return [];

  return value.reduce<AuthFileBatchFailure[]>((result, item) => {
    if (!item || typeof item !== 'object') return result;
    const entry = item as Record<string, unknown>;
    const name = String(entry.name ?? '').trim();
    const error =
      typeof entry.error === 'string'
        ? entry.error.trim()
        : typeof entry.message === 'string'
          ? entry.message.trim()
          : '';

    if (!name && !error) return result;
    result.push({ name, error: error || 'Unknown error' });
    return result;
  }, []);
};

const deriveSuccessfulFileNames = (
  requestedNames: string[],
  failed: AuthFileBatchFailure[]
): string[] => {
  const failedNames = new Set(failed.map((entry) => entry.name.trim()).filter(Boolean));

  if (failedNames.size === 0) {
    return [...requestedNames];
  }

  return requestedNames.filter((name) => !failedNames.has(name));
};

const normalizeBatchUploadResponse = (
  payload: AuthFileBatchUploadResponse | undefined,
  requestedNames: string[]
): AuthFileBatchUploadResult => {
  const failed = normalizeBatchFailures(payload?.failed);
  const uploadedFilesFromPayload = normalizeBatchFileNames(payload?.files);
  const uploaded =
    typeof payload?.uploaded === 'number'
      ? payload.uploaded
      : uploadedFilesFromPayload.length > 0
        ? uploadedFilesFromPayload.length
        : requestedNames.length === 1 && failed.length === 0
          ? 1
          : 0;

  let uploadedFiles = uploadedFilesFromPayload;
  if (uploadedFiles.length === 0 && uploaded > 0) {
    if (failed.length === 0 && uploaded === requestedNames.length) {
      uploadedFiles = [...requestedNames];
    } else {
      const derivedNames = deriveSuccessfulFileNames(requestedNames, failed);
      if (derivedNames.length === uploaded) {
        uploadedFiles = derivedNames;
      }
    }
  }

  return {
    status:
      typeof payload?.status === 'string' ? payload.status : failed.length > 0 ? 'partial' : 'ok',
    uploaded,
    files: uploadedFiles,
    failed,
  };
};

const normalizeBatchDeleteResponse = (
  payload: AuthFileBatchDeleteResponse | undefined,
  requestedNames: string[]
): AuthFileBatchDeleteResult => {
  const failed = normalizeBatchFailures(payload?.failed);
  const deletedFilesFromPayload = normalizeBatchFileNames(payload?.files);
  const deleted =
    typeof payload?.deleted === 'number'
      ? payload.deleted
      : deletedFilesFromPayload.length > 0
        ? deletedFilesFromPayload.length
        : requestedNames.length === 1 && failed.length === 0
          ? 1
          : 0;

  let deletedFiles = deletedFilesFromPayload;
  if (deletedFiles.length === 0 && deleted > 0) {
    if (failed.length === 0 && deleted === requestedNames.length) {
      deletedFiles = [...requestedNames];
    } else {
      const derivedNames = deriveSuccessfulFileNames(requestedNames, failed);
      if (derivedNames.length === deleted) {
        deletedFiles = derivedNames;
      }
    }
  }

  return {
    status:
      typeof payload?.status === 'string' ? payload.status : failed.length > 0 ? 'partial' : 'ok',
    deleted,
    files: deletedFiles,
    failed,
  };
};

const readTextField = (entry: AuthFileEntry, key: string): string => {
  const value = entry[key];
  return typeof value === 'string' ? value.trim() : '';
};

const readAuthIndexField = (entry: AuthFileEntry): string => {
  const value = entry.authIndex ?? entry['auth_index'] ?? entry['auth-index'];
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  if (typeof value === 'string') return value.trim();
  return '';
};

const getAuthFileDedupeKey = (entry: AuthFileEntry): string => {
  const name = readTextField(entry, 'name');
  const authIndex = readAuthIndexField(entry);
  if (name && authIndex) return `${name}\u0000${authIndex}`;
  return name || JSON.stringify(entry);
};

const readDateField = (entry: AuthFileEntry): number => {
  const candidates = [entry['modtime'], entry.modified, entry['updated_at'], entry['last_refresh']];

  for (const value of candidates) {
    if (typeof value === 'number' && Number.isFinite(value)) {
      return value < 1e12 ? value * 1000 : value;
    }
    if (typeof value === 'string') {
      const trimmed = value.trim();
      if (!trimmed) continue;
      const asNumber = Number(trimmed);
      if (Number.isFinite(asNumber)) {
        return asNumber < 1e12 ? asNumber * 1000 : asNumber;
      }
      const parsed = parseTimestampMs(trimmed);
      if (!Number.isNaN(parsed)) {
        return parsed;
      }
    }
  }

  return 0;
};

const isRuntimeOnlyEntry = (entry: AuthFileEntry): boolean => {
  const value = entry['runtime_only'] ?? entry.runtimeOnly;
  if (typeof value === 'boolean') return value;
  if (typeof value === 'string') return value.trim().toLowerCase() === 'true';
  return false;
};

const hasMeaningfulValue = (value: unknown): boolean => {
  if (value == null) return false;
  if (typeof value === 'string') return value.trim().length > 0;
  if (Array.isArray(value)) return value.length > 0;
  return true;
};

const countMeaningfulFields = (entry: AuthFileEntry): number =>
  Object.values(entry).reduce<number>(
    (count, value) => count + (hasMeaningfulValue(value) ? 1 : 0),
    0
  );

const authFilePriorityScore = (entry: AuthFileEntry): number => {
  let score = 0;
  if (readTextField(entry, 'source').toLowerCase() === 'file') score += 32;
  if (readTextField(entry, 'path')) score += 16;
  if (!isRuntimeOnlyEntry(entry)) score += 8;
  if (entry.disabled !== true) score += 4;
  if (readDateField(entry) > 0) score += 2;
  return score;
};

const compareAuthFileEntries = (left: AuthFileEntry, right: AuthFileEntry): number => {
  const scoreDiff = authFilePriorityScore(right) - authFilePriorityScore(left);
  if (scoreDiff !== 0) return scoreDiff;

  const dateDiff = readDateField(right) - readDateField(left);
  if (dateDiff !== 0) return dateDiff;

  const fieldDiff = countMeaningfulFields(right) - countMeaningfulFields(left);
  if (fieldDiff !== 0) return fieldDiff;

  return 0;
};

const mergeAuthFileEntries = (entries: AuthFileEntry[]): AuthFileEntry => {
  const [primary, ...rest] = [...entries].sort(compareAuthFileEntries);
  const merged: AuthFileEntry = { ...primary };

  rest.forEach((entry) => {
    Object.entries(entry).forEach(([key, value]) => {
      if (!hasMeaningfulValue(merged[key]) && hasMeaningfulValue(value)) {
        merged[key] = value;
      }
    });
  });

  return merged;
};

const dedupeAuthFilesResponse = (payload: AuthFilesResponse): AuthFilesResponse => {
  const files = Array.isArray(payload?.files) ? payload.files : [];
  const grouped = new Map<string, AuthFileEntry[]>();

  files.forEach((entry) => {
    const key = getAuthFileDedupeKey(entry);
    const bucket = grouped.get(key);
    if (bucket) {
      bucket.push(entry);
      return;
    }
    grouped.set(key, [entry]);
  });

  const normalizedFiles = Array.from(grouped.values()).map(mergeAuthFileEntries);
  normalizedFiles.sort((left, right) => {
    const nameDiff = readTextField(left, 'name').localeCompare(
      readTextField(right, 'name'),
      undefined,
      {
        sensitivity: 'accent',
      }
    );
    if (nameDiff !== 0) return nameDiff;

    return readAuthIndexField(left).localeCompare(readAuthIndexField(right), undefined, {
      numeric: true,
      sensitivity: 'accent',
    });
  });

  return {
    ...payload,
    files: normalizedFiles,
    total: typeof payload.total === 'number' ? payload.total : normalizedFiles.length,
  };
};

const parseAuthFileJsonObject = (rawText: string): Record<string, unknown> => {
  const trimmed = rawText.trim();

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed) as unknown;
  } catch {
    throw new Error(AUTH_FILE_INVALID_JSON_OBJECT_ERROR);
  }

  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(AUTH_FILE_INVALID_JSON_OBJECT_ERROR);
  }

  return { ...(parsed as Record<string, unknown>) };
};

const parseAuthFileJsonValue = (rawText: string): AuthFileJsonValue => {
  const trimmed = rawText.trim();

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed) as unknown;
  } catch {
    throw new Error(AUTH_FILE_INVALID_JSON_OBJECT_ERROR);
  }

  if (!parsed || typeof parsed !== 'object') {
    throw new Error(AUTH_FILE_INVALID_JSON_OBJECT_ERROR);
  }

  if (Array.isArray(parsed)) {
    if (!parsed.every((item) => item && typeof item === 'object' && !Array.isArray(item))) {
      throw new Error(AUTH_FILE_INVALID_JSON_OBJECT_ERROR);
    }
    return parsed.map((item) => ({ ...(item as Record<string, unknown>) }));
  }

  return { ...(parsed as Record<string, unknown>) };
};

const normalizePatchAuthIndex = (value: unknown): string => {
  if (value === undefined || value === null) return '';
  return String(value).trim();
};

const readPatchRecordAuthIndex = (record: Record<string, unknown>): string =>
  normalizePatchAuthIndex(record.authIndex ?? record['auth_index'] ?? record['auth-index']);

const applyFieldsPatchToAuthRecord = (
  record: Record<string, unknown>,
  fields: AuthFileFieldsPatch
): Record<string, unknown> => {
  const next = { ...record };

  if (fields.prefix !== undefined) {
    const value = fields.prefix.trim();
    if (value) {
      next.prefix = value;
    } else {
      delete next.prefix;
    }
  }

  if (fields.proxy_url !== undefined) {
    const value = fields.proxy_url.trim();
    if (value) {
      next.proxy_url = value;
    } else {
      delete next.proxy_url;
    }
  }

  if (fields.priority !== undefined) {
    if (fields.priority === 0) {
      delete next.priority;
    } else {
      next.priority = fields.priority;
    }
  }

  if (fields.note !== undefined) {
    const value = fields.note.trim();
    if (value) {
      next.note = value;
    } else {
      delete next.note;
    }
  }

  if (fields.headers !== undefined) {
    const currentHeaders =
      next.headers && typeof next.headers === 'object' && !Array.isArray(next.headers)
        ? { ...(next.headers as Record<string, unknown>) }
        : {};
    Object.entries(fields.headers).forEach(([name, rawValue]) => {
      const headerName = name.trim();
      if (!headerName) return;
      const value = rawValue.trim();
      if (value) {
        currentHeaders[headerName] = value;
      } else {
        delete currentHeaders[headerName];
      }
    });
    if (Object.keys(currentHeaders).length > 0) {
      next.headers = currentHeaders;
    } else {
      delete next.headers;
    }
  }

  if (fields.websockets !== undefined) {
    delete next.websocket;
    next.websockets = fields.websockets;
  }

  return next;
};

const patchAuthFileJsonValueByAuthIndexes = (
  value: AuthFileJsonValue,
  authIndexes: AuthFilePatchAuthIndex[],
  fields: AuthFileFieldsPatch
): AuthFileJsonValue => {
  const targetIndexes = new Set(
    authIndexes.map(normalizePatchAuthIndex).filter((authIndex) => authIndex)
  );

  if (targetIndexes.size === 0) {
    return Array.isArray(value)
      ? value.map((record) => applyFieldsPatchToAuthRecord(record, fields))
      : applyFieldsPatchToAuthRecord(value, fields);
  }

  if (!Array.isArray(value)) {
    const recordAuthIndex = readPatchRecordAuthIndex(value);
    if (recordAuthIndex && !targetIndexes.has(recordAuthIndex)) {
      throw new Error('Auth index not found');
    }
    return applyFieldsPatchToAuthRecord(value, fields);
  }

  const matchedIndexes = new Set<string>();
  const nextValue = value.map((record) => {
    const recordAuthIndex = readPatchRecordAuthIndex(record);
    if (!recordAuthIndex || !targetIndexes.has(recordAuthIndex)) {
      return record;
    }
    matchedIndexes.add(recordAuthIndex);
    return applyFieldsPatchToAuthRecord(record, fields);
  });

  if (matchedIndexes.size !== targetIndexes.size) {
    throw new Error('Auth index not found');
  }

  return nextValue;
};

const saveAuthFileText = async (name: string, text: string) => {
  const file = new File([text], name, { type: 'application/json' });
  const result = await authFilesApi.upload(file);
  const normalizedStatus = result.status.trim().toLowerCase();
  const hasExplicitFailureStatus =
    normalizedStatus === 'error' || normalizedStatus === 'failed' || normalizedStatus === 'partial';
  if (hasExplicitFailureStatus || result.failed.length > 0 || result.uploaded === 0) {
    const failure = result.failed[0];
    throw new Error(failure?.error || 'Upload failed');
  }
};

const AUTH_FILES_BULK_PAGE_SIZE = 500;

const listAllAuthFiles = async (
  view: 'summary' | 'snapshot',
  extra: Omit<AuthFilesListQuery, 'view' | 'page' | 'page_size'> = {}
): Promise<AuthFilesResponse> => {
  const files: AuthFileEntry[] = [];
  let page = 1;
  let total = 0;
  let serverTimeMs = 0;
  let snapshotETag = '';
  let facets: AuthFilesResponse['facets'] = undefined;

  while (true) {
    const response = await authFilesApi.list({
      ...extra,
      view,
      page,
      page_size: AUTH_FILES_BULK_PAGE_SIZE,
    });
    files.push(...response.files);
    total = typeof response.total === 'number' ? response.total : files.length;
    if (serverTimeMs <= 0 && (response.server_time_ms ?? 0) > 0) {
      serverTimeMs = response.server_time_ms ?? 0;
    }
    if (!snapshotETag && response.snapshot_etag) {
      snapshotETag = response.snapshot_etag;
    }
    facets = response.facets ?? facets;
    if (!response.has_more && files.length >= total) break;
    if (response.files.length === 0 || response.files.length < AUTH_FILES_BULK_PAGE_SIZE) break;
    page += 1;
  }

  const result = dedupeAuthFilesResponse({
    files,
    total,
    facets,
    server_time_ms: serverTimeMs || undefined,
    snapshot_etag: snapshotETag || undefined,
  });
  useCredentialStatusStore.getState().upsertSnapshots(result.files);
  return result;
};

export const isAuthFileInvalidJsonObjectError = (err: unknown): boolean =>
  err instanceof Error && err.message === AUTH_FILE_INVALID_JSON_OBJECT_ERROR;

const normalizeOauthExcludedModels = (payload: unknown): Record<string, string[]> => {
  if (!payload || typeof payload !== 'object') return {};

  const record = payload as Record<string, unknown>;
  const source = record['oauth-excluded-models'] ?? record.items ?? payload;
  if (!source || typeof source !== 'object') return {};

  const result: Record<string, string[]> = {};

  Object.entries(source as Record<string, unknown>).forEach(([provider, models]) => {
    const key = String(provider ?? '')
      .trim()
      .toLowerCase();
    if (!key) return;

    const rawList = Array.isArray(models)
      ? models
      : typeof models === 'string'
        ? models.split(/[\n,]+/)
        : [];

    const seen = new Set<string>();
    const normalized: string[] = [];
    rawList.forEach((item) => {
      const trimmed = String(item ?? '').trim();
      if (!trimmed) return;
      const modelKey = trimmed.toLowerCase();
      if (seen.has(modelKey)) return;
      seen.add(modelKey);
      normalized.push(trimmed);
    });

    result[key] = normalized;
  });

  return result;
};

const normalizeOauthModelAlias = (payload: unknown): Record<string, OAuthModelAliasEntry[]> => {
  if (!payload || typeof payload !== 'object') return {};

  const record = payload as Record<string, unknown>;
  const source = record['oauth-model-alias'] ?? record.items ?? payload;
  if (!source || typeof source !== 'object') return {};

  const result: Record<string, OAuthModelAliasEntry[]> = {};

  Object.entries(source as Record<string, unknown>).forEach(([channel, mappings]) => {
    const key = String(channel ?? '')
      .trim()
      .toLowerCase();
    if (!key) return;
    if (!Array.isArray(mappings)) return;

    const seen = new Set<string>();
    const normalized = mappings
      .map((item) => {
        if (!item || typeof item !== 'object') return null;
        const entry = item as Record<string, unknown>;
        const name = String(entry.name ?? entry.id ?? entry.model ?? '').trim();
        const alias = String(entry.alias ?? '').trim();
        if (!name || !alias) return null;
        const fork = entry.fork === true;
        const forceMapping =
          entry['force-mapping'] === true ||
          entry.forceMapping === true ||
          entry.force_mapping === true;
        return {
          name,
          alias,
          ...(fork ? { fork: true } : {}),
          ...(forceMapping ? { forceMapping: true } : {}),
        };
      })
      .filter(Boolean)
      .filter((entry) => {
        const aliasEntry = entry as OAuthModelAliasEntry;
        const dedupeKey = `${aliasEntry.name.toLowerCase()}::${aliasEntry.alias.toLowerCase()}::${aliasEntry.fork ? '1' : '0'}::${aliasEntry.forceMapping ? '1' : '0'}`;
        if (seen.has(dedupeKey)) return false;
        seen.add(dedupeKey);
        return true;
      }) as OAuthModelAliasEntry[];

    if (normalized.length) {
      result[key] = normalized;
    }
  });

  return result;
};

const OAUTH_MODEL_ALIAS_ENDPOINT = '/oauth-model-alias';

const readPageInteger = (value: unknown): number | null => {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
};

const fetchAllPagedMap = async <T>(
  endpoint: string,
  normalize: (payload: unknown) => Record<string, T[]>
): Promise<Record<string, T[]>> => {
  let payload = await apiClient.get<unknown>(endpoint);
  const merged: Record<string, T[]> = {};

  while (true) {
    const pageItems = normalize(payload);
    Object.entries(pageItems).forEach(([key, values]) => {
      merged[key] = [...(merged[key] ?? []), ...values];
    });

    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) break;
    const pageRecord = payload as Record<string, unknown>;
    if (pageRecord.has_more !== true) break;
    const page = readPageInteger(pageRecord.page) ?? 1;
    const pageSize = readPageInteger(pageRecord.page_size) ?? 100;
    const totalPages = readPageInteger(pageRecord.total_pages);
    if (totalPages && page >= totalPages) break;
    payload = await apiClient.get<unknown>(endpoint, {
      params: { page: page + 1, page_size: pageSize },
    });
  }

  return merged;
};

const extractModelDefinitions = (
  payload: unknown
): { id: string; display_name?: string; type?: string; owned_by?: string }[] => {
  if (!payload || typeof payload !== 'object') return [];
  const record = payload as Record<string, unknown>;
  const models = record.models ?? record['models'];
  return Array.isArray(models)
    ? (models as { id: string; display_name?: string; type?: string; owned_by?: string }[])
    : [];
};

export const authFilesApi = {
  list: async (params?: AuthFilesListQuery) => {
    const response = dedupeAuthFilesResponse(
      params
        ? await apiClient.get<AuthFilesResponse>('/auth-files', { params })
        : await apiClient.get<AuthFilesResponse>('/auth-files')
    );
    useCredentialStatusStore.getState().upsertSnapshots(response.files);
    return response;
  },

  listSummary: () => listAllAuthFiles('summary'),

  listSnapshot: (updatedAfterMs?: number) =>
    listAllAuthFiles(
      'snapshot',
      typeof updatedAfterMs === 'number' && updatedAfterMs > 0
        ? { updated_after_ms: updatedAfterMs }
        : {}
    ),

  count: async (): Promise<number> => {
    const response = await authFilesApi.list({ view: 'count', page: 1, page_size: 1 });
    return typeof response.total === 'number' ? response.total : response.files.length;
  },

  patchFile: async (payload: AuthFilePatchPayload) => {
    const response = await apiClient.patch<AuthFileStatusResponse>('/auth-files', payload);
    if (typeof payload.disabled === 'boolean') {
      useCredentialStatusStore.getState().patchStatus({ name: payload.name }, payload.disabled);
    }
    return response;
  },

  setStatus: async (name: string, disabled: boolean, authIndex?: string | number | null) => {
    const normalizedAuthIndex = normalizePatchAuthIndex(authIndex);
    const response = await apiClient.patch<AuthFileStatusResponse>('/auth-files/status', {
      name,
      disabled,
      ...(normalizedAuthIndex ? { auth_index: normalizedAuthIndex } : {}),
    });
    useCredentialStatusStore
      .getState()
      .patchStatus({ name, authIndex: normalizedAuthIndex }, disabled);
    return response;
  },

  setStatuses: async (
    targets: AuthFileStatusTarget[],
    disabled: boolean
  ): Promise<AuthFileBatchStatusResult> => {
    const seen = new Set<string>();
    const items = targets.reduce<Array<{ name: string; auth_index?: string }>>((result, target) => {
      const name = String(target?.name ?? '').trim();
      const authIndex = normalizePatchAuthIndex(target?.authIndex);
      if (!name) return result;
      const key = `${name}\u0000${authIndex}`;
      if (seen.has(key)) return result;
      seen.add(key);
      result.push({ name, ...(authIndex ? { auth_index: authIndex } : {}) });
      return result;
    }, []);
    if (items.length === 0) {
      return { status: 'ok', updated: 0, items: [], failed: [] };
    }

    const payload = await apiClient.patch<AuthFileBatchStatusResponse>('/auth-files/status/batch', {
      items,
      disabled,
    });
    const updatedItems = Array.isArray(payload?.items)
      ? (payload.items as AuthFileBatchStatusItem[]).reduce<AuthFileBatchStatusResult['items']>(
          (result, item) => {
            const name = String(item?.name ?? '').trim();
            const authIndex = normalizePatchAuthIndex(item?.auth_index);
            if (!name || typeof item?.disabled !== 'boolean') return result;
            result.push({ name, authIndex, disabled: item.disabled });
            return result;
          },
          []
        )
      : [];
    const failed = Array.isArray(payload?.failed)
      ? (payload.failed as AuthFileBatchStatusFailure[]).reduce<AuthFileBatchFailure[]>(
          (result, item) => {
            const name = String(item?.name ?? '').trim();
            const error = String(item?.error ?? '').trim();
            if (!name && !error) return result;
            result.push({ name, error: error || 'Unknown error' });
            return result;
          },
          []
        )
      : [];
    updatedItems.forEach((item) => {
      useCredentialStatusStore
        .getState()
        .patchStatus({ name: item.name, authIndex: item.authIndex }, item.disabled);
    });
    return {
      status:
        typeof payload?.status === 'string' ? payload.status : failed.length ? 'partial' : 'ok',
      updated:
        typeof payload?.updated === 'number' && Number.isFinite(payload.updated)
          ? payload.updated
          : updatedItems.length,
      items: updatedItems,
      failed,
    };
  },

  setStatusWithFallback: async (
    name: string,
    disabled: boolean,
    authIndex?: string | number | null
  ) => {
    try {
      if (normalizePatchAuthIndex(authIndex)) {
        return await authFilesApi.setStatus(name, disabled, authIndex);
      }
      const response = await authFilesApi.patchFile({ name, disabled });
      useCredentialStatusStore.getState().patchStatus({ name }, disabled);
      return response;
    } catch {
      return authFilesApi.setStatus(name, disabled, authIndex);
    }
  },

  patchFields: (name: string, fields: AuthFileFieldsPatch) =>
    apiClient.patch('/auth-files/fields', { name, ...fields }),

  patchFieldsBatch: async (
    targets: AuthFileFieldsTarget[],
    fields: AuthFileFieldsPatch
  ): Promise<AuthFileBatchFieldsResult> => {
    const items = targets.reduce<Array<{ name: string; auth_indices?: string[] }>>(
      (result, target) => {
        const name = String(target?.name ?? '').trim();
        if (!name) return result;
        const authIndexes = Array.from(
          new Set((target.authIndexes ?? []).map(normalizePatchAuthIndex).filter(Boolean))
        );
        result.push({ name, ...(authIndexes.length ? { auth_indices: authIndexes } : {}) });
        return result;
      },
      []
    );
    if (items.length === 0 || Object.keys(fields).length === 0) {
      return { status: 'ok', updated: 0, failed: [] };
    }
    const payload = await apiClient.patch<AuthFileBatchFieldsResponse>('/auth-files/fields/batch', {
      items,
      fields,
    });
    const failed = normalizeBatchFailures(payload?.failed);
    return {
      status:
        typeof payload?.status === 'string' ? payload.status : failed.length ? 'partial' : 'ok',
      updated:
        typeof payload?.updated === 'number' && Number.isFinite(payload.updated)
          ? payload.updated
          : Math.max(0, items.length - failed.length),
      failed,
    };
  },

  patchFieldsForAuthIndexes: async (
    name: string,
    authIndexes: AuthFilePatchAuthIndex[],
    fields: AuthFileFieldsPatch
  ) => {
    const rawText = await authFilesApi.downloadText(name);
    const value = parseAuthFileJsonValue(rawText);
    const nextValue = patchAuthFileJsonValueByAuthIndexes(value, authIndexes, fields);
    await authFilesApi.saveJsonObject(name, nextValue);
  },

  uploadFiles: async (files: File[]): Promise<AuthFileBatchUploadResult> => {
    const requestedNames = files.map((file) => file.name);
    if (requestedNames.length === 0) {
      return { status: 'ok', uploaded: 0, files: [], failed: [] };
    }

    const formData = new FormData();
    const formField = files.length === 1 ? 'file' : 'files';
    files.forEach((file) => formData.append(formField, file, file.name));
    const payload = await apiClient.postForm<AuthFileBatchUploadResponse>('/auth-files', formData);
    return normalizeBatchUploadResponse(payload, requestedNames);
  },

  upload: (file: File) => authFilesApi.uploadFiles([file]),

  deleteFiles: async (names: string[]): Promise<AuthFileBatchDeleteResult> => {
    const requestedNames = normalizeRequestedAuthFileNames(names);
    if (requestedNames.length === 0) {
      return { status: 'ok', deleted: 0, files: [], failed: [] };
    }

    const payload = await apiClient.delete<AuthFileBatchDeleteResponse>('/auth-files', {
      data: { names: requestedNames },
    });
    const result = normalizeBatchDeleteResponse(payload, requestedNames);
    useCredentialStatusStore.getState().removeFiles(result.files);
    return result;
  },

  deleteFile: (name: string) => authFilesApi.deleteFiles([name]),

  deleteFileByName: async (name: string): Promise<AuthFileBatchDeleteResult> => {
    const requestedNames = normalizeRequestedAuthFileNames([name]);
    if (requestedNames.length === 0) {
      return { status: 'ok', deleted: 0, files: [], failed: [] };
    }

    const payload = await apiClient.delete<AuthFileBatchDeleteResponse>(
      `/auth-files?name=${encodeURIComponent(requestedNames[0])}`
    );
    const result = normalizeBatchDeleteResponse(payload, requestedNames);
    useCredentialStatusStore.getState().removeFiles(result.files);
    return result;
  },

  deleteAll: async () => {
    const response = await apiClient.delete('/auth-files', { params: { all: true } });
    useCredentialStatusStore.getState().replaceSnapshots([]);
    return response;
  },

  downloadFiles: async (names: string[]): Promise<AuthFileBatchDownloadResult> => {
    const requestedNames = normalizeRequestedAuthFileNames(names);
    if (requestedNames.length === 0) {
      return {
        blob: new Blob([], { type: 'application/zip' }),
        filename: 'auth-files.zip',
        included: 0,
        failed: 0,
      };
    }
    const response = await apiClient.requestRaw({
      method: 'POST',
      url: '/auth-files/download',
      data: { names: requestedNames },
      responseType: 'blob',
    });
    const readHeader = (name: string): string => {
      const headers = response.headers as unknown as {
        get?: (key: string) => unknown;
        [key: string]: unknown;
      };
      const direct = headers?.get?.(name) ?? headers?.[name] ?? headers?.[name.toLowerCase()];
      return direct == null ? '' : String(direct);
    };
    const contentDisposition = readHeader('content-disposition');
    const filenameMatch = /filename="?([^";]+)"?/i.exec(contentDisposition);
    const included = Number.parseInt(readHeader('x-auth-files-included'), 10);
    const failed = Number.parseInt(readHeader('x-auth-files-failed'), 10);
    return {
      blob:
        response.data instanceof Blob
          ? response.data
          : new Blob([response.data], { type: 'application/zip' }),
      filename: filenameMatch?.[1]?.trim() || 'auth-files.zip',
      included: Number.isFinite(included) ? included : requestedNames.length,
      failed: Number.isFinite(failed) ? failed : 0,
    };
  },

  downloadText: async (name: string): Promise<string> => {
    const response = await apiClient.getRaw(
      `/auth-files/download?name=${encodeURIComponent(name)}`,
      {
        responseType: 'blob',
      }
    );
    const blob = response.data as Blob;
    return blob.text();
  },

  async downloadJsonObject(name: string): Promise<Record<string, unknown>> {
    const rawText = await authFilesApi.downloadText(name);
    return parseAuthFileJsonObject(rawText);
  },

  saveText: (name: string, text: string) => saveAuthFileText(name, text),

  saveJsonObject: (name: string, json: AuthFileJsonValue) =>
    saveAuthFileText(name, JSON.stringify(json)),

  // OAuth 排除模型
  async getOauthExcludedModels(): Promise<Record<string, string[]>> {
    return fetchAllPagedMap('/oauth-excluded-models', normalizeOauthExcludedModels);
  },

  saveOauthExcludedModels: (provider: string, models: string[]) =>
    apiClient.patch('/oauth-excluded-models', { provider, models }),

  deleteOauthExcludedEntry: (provider: string) =>
    apiClient.delete(`/oauth-excluded-models?provider=${encodeURIComponent(provider)}`),

  replaceOauthExcludedModels: (map: Record<string, string[]>) =>
    apiClient.put('/oauth-excluded-models', normalizeOauthExcludedModels(map)),

  // OAuth 模型别名
  async getOauthModelAlias(): Promise<Record<string, OAuthModelAliasEntry[]>> {
    return fetchAllPagedMap(OAUTH_MODEL_ALIAS_ENDPOINT, normalizeOauthModelAlias);
  },

  saveOauthModelAlias: async (channel: string, aliases: OAuthModelAliasEntry[]) => {
    const normalizedChannel = String(channel ?? '')
      .trim()
      .toLowerCase();
    const normalizedAliases =
      normalizeOauthModelAlias({ [normalizedChannel]: aliases })[normalizedChannel] ?? [];
    await apiClient.patch(OAUTH_MODEL_ALIAS_ENDPOINT, {
      channel: normalizedChannel,
      aliases: normalizedAliases.map(({ forceMapping, ...entry }) => ({
        ...entry,
        ...(forceMapping ? { 'force-mapping': true } : {}),
      })),
    });
  },

  deleteOauthModelAlias: async (channel: string) => {
    const normalizedChannel = String(channel ?? '')
      .trim()
      .toLowerCase();

    try {
      await apiClient.patch(OAUTH_MODEL_ALIAS_ENDPOINT, {
        channel: normalizedChannel,
        aliases: [],
      });
    } catch (err: unknown) {
      const status = getStatusCode(err);
      if (status !== 405) throw err;
      await apiClient.delete(
        `${OAUTH_MODEL_ALIAS_ENDPOINT}?channel=${encodeURIComponent(normalizedChannel)}`
      );
    }
  },

  // 获取认证凭证支持的模型
  async getModelsForAuthFile(
    name: string
  ): Promise<{ id: string; display_name?: string; type?: string; owned_by?: string }[]> {
    const endpoint = '/auth-files/models';
    let data = await apiClient.get<unknown>(endpoint, { params: { name } });
    const models = extractModelDefinitions(data);
    while (data && typeof data === 'object' && !Array.isArray(data)) {
      const record = data as Record<string, unknown>;
      if (record.has_more !== true) break;
      const page = readPageInteger(record.page) ?? 1;
      const pageSize = readPageInteger(record.page_size) ?? 100;
      const totalPages = readPageInteger(record.total_pages);
      if (totalPages && page >= totalPages) break;
      data = await apiClient.get<unknown>(endpoint, {
        params: { name, page: page + 1, page_size: pageSize },
      });
      models.push(...extractModelDefinitions(data));
    }
    return models;
  },

  // 获取指定 channel 的模型定义
  async getModelDefinitions(
    channel: string
  ): Promise<{ id: string; display_name?: string; type?: string; owned_by?: string }[]> {
    const normalizedChannel = String(channel ?? '')
      .trim()
      .toLowerCase();
    if (!normalizedChannel) return [];
    const endpoint = `/model-definitions/${encodeURIComponent(normalizedChannel)}`;
    let data = await apiClient.get<unknown>(endpoint);
    const models = extractModelDefinitions(data);
    while (data && typeof data === 'object' && !Array.isArray(data)) {
      const record = data as Record<string, unknown>;
      if (record.has_more !== true) break;
      const page = readPageInteger(record.page) ?? 1;
      const pageSize = readPageInteger(record.page_size) ?? 100;
      const totalPages = readPageInteger(record.total_pages);
      if (totalPages && page >= totalPages) break;
      data = await apiClient.get<unknown>(endpoint, {
        params: { page: page + 1, page_size: pageSize },
      });
      models.push(...extractModelDefinitions(data));
    }
    return models;
  },
};
