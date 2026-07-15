import { authFilesApi } from '@/services/api/authFiles';
import { useCredentialStatusStore } from '@/stores/useCredentialStatusStore';

const FULL_REFRESH_INTERVAL_MS = 5 * 60 * 1000;

let activeScope = '';
let updatedAfterMs = 0;
let lastFullRefreshAt = 0;
let snapshotETag = '';
let refreshPromise: Promise<void> | null = null;
let refreshScope = '';

export const activateCredentialStatusSyncScope = (scope: string): void => {
  const normalized = scope.trim();
  if (activeScope === normalized) return;
  activeScope = normalized;
  updatedAfterMs = 0;
  lastFullRefreshAt = 0;
  snapshotETag = '';
  useCredentialStatusStore.getState().activateScope(normalized);
};

export const clearCredentialStatusSync = (): void => {
  activeScope = '';
  updatedAfterMs = 0;
  lastFullRefreshAt = 0;
  snapshotETag = '';
  refreshPromise = null;
  refreshScope = '';
  useCredentialStatusStore.getState().clear();
};

export const refreshCredentialStatuses = async (options?: {
  forceFull?: boolean;
}): Promise<void> => {
  if (!activeScope) return;
  if (refreshPromise) {
    if (refreshScope === activeScope) return refreshPromise;
    await refreshPromise.catch(() => undefined);
    return refreshCredentialStatuses(options);
  }

  const scope = activeScope;
  const task = (async () => {
    const now = Date.now();
    let fullRefresh =
      options?.forceFull === true ||
      updatedAfterMs <= 0 ||
      (!snapshotETag && now - lastFullRefreshAt >= FULL_REFRESH_INTERVAL_MS);
    let response = await authFilesApi.listSnapshot(fullRefresh ? undefined : updatedAfterMs);
    if (activeScope !== scope) return;
    if (
      !fullRefresh &&
      snapshotETag &&
      response.snapshot_etag &&
      response.snapshot_etag !== snapshotETag
    ) {
      response = await authFilesApi.listSnapshot();
      if (activeScope !== scope) return;
      fullRefresh = true;
    }
    if (fullRefresh) {
      useCredentialStatusStore.getState().replaceSnapshots(response.files);
      lastFullRefreshAt = now;
    } else {
      useCredentialStatusStore.getState().upsertSnapshots(response.files);
    }
    if (response.snapshot_etag) {
      snapshotETag = response.snapshot_etag;
    }
    updatedAfterMs = response.server_time_ms ?? Date.now();
  })();
  refreshPromise = task;
  refreshScope = scope;
  try {
    return await task;
  } finally {
    if (refreshPromise === task) {
      refreshPromise = null;
      refreshScope = '';
    }
  }
};
