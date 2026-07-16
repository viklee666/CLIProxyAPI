import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import {
  authFilesApi,
  type AuthFileFieldsPatch,
  type AuthFileJsonUpload,
  type AuthFilesListQuery,
} from '@/services/api';
import { apiClient } from '@/services/api/client';
import { useNotificationStore } from '@/stores';
import {
  applyCredentialStatuses,
  useCredentialStatusStore,
} from '@/stores/useCredentialStatusStore';
import type { AuthFileItem } from '@/types';
import { formatFileSize } from '@/utils/format';
import { MAX_AUTH_FILE_SIZE } from '@/utils/constants';
import { downloadBlob } from '@/utils/download';
import { loadAuthJsonConverter } from '@/features/authFiles/authJsonConverterLoader';
import type {
  AuthJsonConversionResult,
  AuthJsonDetectionType,
  AuthJsonInputType,
  AuthJsonRecord,
} from '@/features/authFiles/authJsonTypes';
import {
  getTypeLabel,
  hasAuthFileStatusMessage,
  isHealthyAuthFile,
  isRuntimeOnlyAuthFile,
  normalizeProviderKey,
} from '@/features/authFiles/constants';
import {
  getAuthFileNameFromSelectionKey,
  getAuthFileSelectionKey,
  type AuthFilePatchTarget,
} from '@/features/authFiles/model/authFilesPageModel';

type DeleteAllOptions = {
  filter: string;
  problemOnly: boolean;
  disabledOnly: boolean;
  healthyOnly: boolean;
  filteredFiles?: AuthFileItem[];
  onResetFilterToAll: () => void;
  onResetProblemOnly: () => void;
  onResetDisabledOnly: () => void;
  onResetHealthyOnly: () => void;
  onResetResultFilters?: () => void;
};

export type AuthFilesBatchPatchResult = {
  success: number;
  failed: number;
  failedNames: string[];
};

export type UseAuthFilesDataResult = {
  files: AuthFileItem[];
  total: number;
  providerFacets: Record<string, number>;
  serverPaged: boolean;
  selectedFiles: Set<string>;
  selectionCount: number;
  loading: boolean;
  error: string;
  uploading: boolean;
  authJsonPasteSaving: boolean;
  deleting: string | null;
  deletingAll: boolean;
  statusUpdating: Record<string, boolean>;
  batchStatusUpdating: boolean;
  batchFieldsUpdating: boolean;
  fileInputRef: RefObject<HTMLInputElement | null>;
  loadFiles: (options?: { throwOnError?: boolean }) => Promise<void>;
  prepareAuthJsonConverter: () => Promise<void>;
  handleUploadClick: () => void;
  handleFileChange: (event: ChangeEvent<HTMLInputElement>) => Promise<void>;
  savePastedAuthJson: (
    type: AuthJsonInputType,
    fileName: string,
    jsonText: string
  ) => Promise<string>;
  handleDelete: (name: string) => void;
  handleDeleteAll: (options: DeleteAllOptions) => void;
  handleDownload: (name: string) => Promise<void>;
  handleStatusToggle: (item: AuthFileItem, enabled: boolean) => Promise<void>;
  toggleSelect: (key: string) => void;
  selectAllVisible: (visibleFiles: AuthFileItem[]) => void;
  invertVisibleSelection: (visibleFiles: AuthFileItem[]) => void;
  deselectAll: () => void;
  batchDownload: (names: string[]) => Promise<void>;
  batchSetStatus: (names: string[], enabled: boolean) => Promise<void>;
  batchPatchFields: (
    targets: AuthFilePatchTarget[],
    fields: AuthFileFieldsPatch
  ) => Promise<AuthFilesBatchPatchResult | null>;
  batchDelete: (names: string[]) => void;
};

const DEFAULT_AUTH_JSON_FILE_NAME = 'codex-account.json';
const AUTH_JSON_UPLOAD_BATCH_FILES = 20;
const AUTH_JSON_UPLOAD_BATCH_BYTES = 2 * 1024 * 1024;

type AuthFilePatchTargetGroup = {
  name: string;
  targets: AuthFilePatchTarget[];
  authIndexes: Array<string | number>;
};

const normalizePatchTargetAuthIndex = (
  value: AuthFilePatchTarget['authIndex']
): string | number | null => {
  if (value === undefined || value === null) return null;
  const trimmed = String(value).trim();
  if (!trimmed) return null;
  return typeof value === 'number' ? value : trimmed;
};

const getPatchTargetKey = (target: AuthFilePatchTarget): string => {
  const authIndex = normalizePatchTargetAuthIndex(target.authIndex);
  return `${target.name}\u0000${authIndex === null ? '-' : String(authIndex)}`;
};

const normalizeBatchPatchTargets = (targets: AuthFilePatchTarget[]): AuthFilePatchTarget[] => {
  const seen = new Set<string>();
  const normalized: AuthFilePatchTarget[] = [];

  targets.forEach((target) => {
    const name = String(target.name ?? '').trim();
    if (!name) return;
    const authIndex = normalizePatchTargetAuthIndex(target.authIndex);
    const normalizedTarget = authIndex === null ? { name } : { name, authIndex };
    const key = getPatchTargetKey(normalizedTarget);
    if (seen.has(key)) return;
    seen.add(key);
    normalized.push(normalizedTarget);
  });

  return normalized;
};

const groupBatchPatchTargets = (targets: AuthFilePatchTarget[]): AuthFilePatchTargetGroup[] => {
  const groups = new Map<string, AuthFilePatchTargetGroup>();

  targets.forEach((target) => {
    const group = groups.get(target.name) ?? {
      name: target.name,
      targets: [],
      authIndexes: [],
    };
    group.targets.push(target);
    const authIndex = normalizePatchTargetAuthIndex(target.authIndex);
    if (authIndex !== null) {
      group.authIndexes.push(authIndex);
    }
    groups.set(target.name, group);
  });

  return Array.from(groups.values());
};

const authJsonRecords = (authJson: AuthJsonConversionResult): AuthJsonRecord[] =>
  Array.isArray(authJson) ? authJson : [authJson];

const numberedAuthFileName = (fileName: string, index: number) => {
  const suffix = String(index + 1).padStart(3, '0');
  const baseName = fileName.toLowerCase().endsWith('.json') ? fileName.slice(0, -5) : fileName;
  return `${baseName}-${suffix}.json`;
};

const uniqueAuthFileName = (name: string, usedNames: Set<string>) => {
  if (!usedNames.has(name)) {
    usedNames.add(name);
    return name;
  }
  const baseName = name.toLowerCase().endsWith('.json') ? name.slice(0, -5) : name;
  let suffix = 2;
  while (usedNames.has(`${baseName}-${suffix}.json`)) suffix += 1;
  const uniqueName = `${baseName}-${suffix}.json`;
  usedNames.add(uniqueName);
  return uniqueName;
};

export const buildConvertedAuthJsonUploads = async (
  type: AuthJsonDetectionType,
  fileName: string,
  jsonText: string
): Promise<AuthFileJsonUpload[]> => {
  const converter = await loadAuthJsonConverter();
  const authJson = converter.convertAuthJsonInput(jsonText, type);
  const records = authJsonRecords(authJson);
  const usedNames = new Set<string>();
  return records.map((record, index) => {
    let resolvedFileName = fileName;
    if (records.length === 1) {
      if (fileName === DEFAULT_AUTH_JSON_FILE_NAME && type === 'session') {
        resolvedFileName = converter.getDefaultSessionAuthFileName(record);
      } else if (fileName === DEFAULT_AUTH_JSON_FILE_NAME && type === 'sub2api') {
        resolvedFileName = converter.getDefaultSub2ApiAuthFileName(record);
      }
    } else if (fileName === DEFAULT_AUTH_JSON_FILE_NAME) {
      resolvedFileName = converter.getDefaultSessionAuthFileName(record);
    } else {
      resolvedFileName = numberedAuthFileName(fileName, index);
    }
    return {
      name: uniqueAuthFileName(resolvedFileName, usedNames),
      text: JSON.stringify(record),
    };
  });
};

const chunkAuthJsonUploads = (files: AuthFileJsonUpload[]): AuthFileJsonUpload[][] => {
  const batches: AuthFileJsonUpload[][] = [];
  let batch: AuthFileJsonUpload[] = [];
  let batchBytes = 0;
  files.forEach((file) => {
    const fileBytes = new Blob([file.text]).size;
    if (
      batch.length > 0 &&
      (batch.length >= AUTH_JSON_UPLOAD_BATCH_FILES ||
        batchBytes + fileBytes > AUTH_JSON_UPLOAD_BATCH_BYTES)
    ) {
      batches.push(batch);
      batch = [];
      batchBytes = 0;
    }
    batch.push(file);
    batchBytes += fileBytes;
  });
  if (batch.length > 0) batches.push(batch);
  return batches;
};

export function useAuthFilesData(query?: AuthFilesListQuery): UseAuthFilesDataResult {
  const { t } = useTranslation();
  const { showNotification, showConfirmation } = useNotificationStore();

  const [files, setFiles] = useState<AuthFileItem[]>([]);
  const [total, setTotal] = useState(0);
  const [providerFacets, setProviderFacets] = useState<Record<string, number>>({});
  const serverPaged = typeof query?.page === 'number' || typeof query?.page_size === 'number';
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [uploading, setUploading] = useState(false);
  const [authJsonPasteSaving, setAuthJsonPasteSaving] = useState(false);
  const authJsonPasteSavingRef = useRef(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [deletingAll, setDeletingAll] = useState(false);
  const [statusUpdating, setStatusUpdating] = useState<Record<string, boolean>>({});
  const [batchStatusUpdating, setBatchStatusUpdating] = useState(false);
  const [batchFieldsUpdating, setBatchFieldsUpdating] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const credentialStatuses = useCredentialStatusStore((state) => state.snapshots);
  const syncedFiles = useMemo(
    () => applyCredentialStatuses(files, credentialStatuses),
    [credentialStatuses, files]
  );

  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const batchStatusPendingRef = useRef(false);
  const batchFieldsPendingRef = useRef(false);
  const selectionCount = selectedFiles.size;
  const toggleSelect = useCallback((key: string) => {
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const selectAllVisible = useCallback((visibleFiles: AuthFileItem[]) => {
    const nextSelected = visibleFiles
      .filter((file) => !isRuntimeOnlyAuthFile(file))
      .map(getAuthFileSelectionKey);
    if (nextSelected.length === 0) return;
    setSelectedFiles((prev) => {
      const next = new Set(prev);
      nextSelected.forEach((key) => next.add(key));
      return next;
    });
  }, []);

  const invertVisibleSelection = useCallback((visibleFiles: AuthFileItem[]) => {
    const visibleNames = visibleFiles
      .filter((file) => !isRuntimeOnlyAuthFile(file))
      .map(getAuthFileSelectionKey);
    if (visibleNames.length === 0) return;

    setSelectedFiles((prev) => {
      const next = new Set(prev);
      visibleNames.forEach((key) => {
        if (next.has(key)) {
          next.delete(key);
        } else {
          next.add(key);
        }
      });
      return next;
    });
  }, []);

  const deselectAll = useCallback(() => {
    setSelectedFiles(new Set());
  }, []);

  const applyDeletedFiles = useCallback((names: string[]) => {
    const deletedNames = Array.from(new Set(names.map((name) => name.trim()).filter(Boolean)));
    if (deletedNames.length === 0) return;

    const deletedSet = new Set(deletedNames);
    setFiles((prev) => prev.filter((file) => !deletedSet.has(file.name)));
    setSelectedFiles((prev) => {
      if (prev.size === 0) return prev;
      let changed = false;
      const next = new Set<string>();
      prev.forEach((key) => {
        const name = getAuthFileNameFromSelectionKey(key);
        if (deletedSet.has(name)) {
          changed = true;
        } else {
          next.add(key);
        }
      });
      return changed ? next : prev;
    });
  }, []);

  useEffect(() => {
    if (selectedFiles.size === 0) return;
    const existingKeys = new Set(files.map(getAuthFileSelectionKey));
    setSelectedFiles((prev) => {
      let changed = false;
      const next = new Set<string>();
      prev.forEach((key) => {
        if (existingKeys.has(key)) {
          next.add(key);
        } else {
          changed = true;
        }
      });
      return changed ? next : prev;
    });
  }, [files, selectedFiles.size]);

  const loadFiles = useCallback(
    async (options?: { throwOnError?: boolean }) => {
      setLoading(true);
      setError('');
      try {
        const data = query ? await authFilesApi.list(query) : await authFilesApi.listSummary();
        setFiles(data?.files || []);
        setTotal(typeof data?.total === 'number' ? data.total : data?.files?.length || 0);
        setProviderFacets(data?.facets?.providers || {});
      } catch (err: unknown) {
        const errorMessage = err instanceof Error ? err.message : t('notification.refresh_failed');
        setError(errorMessage);
        if (options?.throwOnError) {
          throw err instanceof Error ? err : new Error(errorMessage);
        }
      } finally {
        setLoading(false);
      }
    },
    [query, t]
  );

  const prepareAuthJsonConverter = useCallback(async () => {
    await loadAuthJsonConverter();
  }, []);

  const uploadAuthJsonBatches = useCallback(async (filesToUpload: AuthFileJsonUpload[]) => {
    const uploadedFiles: string[] = [];
    const failed: Array<{ name: string; error: string }> = [];
    for (const batch of chunkAuthJsonUploads(filesToUpload)) {
      try {
        const result = await authFilesApi.uploadJsonTexts(batch);
        uploadedFiles.push(...result.files);
        failed.push(...result.failed);
      } catch {
        failed.push(...batch.map((file) => ({ name: file.name, error: 'Upload failed' })));
      }
    }
    return { uploadedFiles, failed };
  }, []);

  const handleUploadClick = useCallback(() => {
    void prepareAuthJsonConverter();
    fileInputRef.current?.click();
  }, [prepareAuthJsonConverter]);

  const handleFileChange = useCallback(
    async (event: ChangeEvent<HTMLInputElement>) => {
      const fileList = event.target.files;
      if (!fileList || fileList.length === 0) return;

      const filesToUpload = Array.from(fileList);
      const validFiles: File[] = [];
      const invalidFiles: string[] = [];
      const oversizedFiles: string[] = [];

      filesToUpload.forEach((file) => {
        if (!file.name.endsWith('.json')) {
          invalidFiles.push(file.name);
          return;
        }
        if (file.size > MAX_AUTH_FILE_SIZE) {
          oversizedFiles.push(file.name);
          return;
        }
        validFiles.push(file);
      });

      if (invalidFiles.length > 0) {
        showNotification(t('auth_files.upload_error_json'), 'error');
      }
      if (oversizedFiles.length > 0) {
        showNotification(
          t('auth_files.upload_error_size', { maxSize: formatFileSize(MAX_AUTH_FILE_SIZE) }),
          'error'
        );
      }

      if (validFiles.length === 0) {
        event.target.value = '';
        return;
      }

      setUploading(true);
      try {
        const convertedFiles: AuthFileJsonUpload[] = [];
        const conversionFailures: Array<{ name: string; error: string }> = [];
        for (const file of validFiles) {
          try {
            convertedFiles.push(
              ...(await buildConvertedAuthJsonUploads('auto', file.name, await file.text()))
            );
          } catch (error) {
            conversionFailures.push({
              name: file.name,
              error: error instanceof Error ? error.message : 'Invalid auth JSON',
            });
          }
        }

        const result = await uploadAuthJsonBatches(convertedFiles);
        const failures = [...conversionFailures, ...result.failed];
        const successCount = result.uploadedFiles.length;

        if (successCount > 0) {
          const total = convertedFiles.length + conversionFailures.length;
          const suffix = total > 1 ? ` (${successCount}/${total})` : '';
          showNotification(
            `${t('auth_files.upload_success')}${suffix}`,
            failures.length ? 'warning' : 'success'
          );
          await loadFiles();
        }

        if (failures.length > 0) {
          const details = failures.map((item) => `${item.name}: ${item.error}`).join('; ');
          showNotification(`${t('notification.upload_failed')}: ${details}`, 'error');
        }
      } catch (err: unknown) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        showNotification(`${t('notification.upload_failed')}: ${errorMessage}`, 'error');
      } finally {
        setUploading(false);
        event.target.value = '';
      }
    },
    [loadFiles, showNotification, t, uploadAuthJsonBatches]
  );

  const savePastedAuthJson = useCallback(
    async (type: AuthJsonInputType, fileName: string, jsonText: string) => {
      if (authJsonPasteSavingRef.current) {
        throw new Error(t('auth_files.paste_error_save_in_progress'));
      }
      authJsonPasteSavingRef.current = true;
      setAuthJsonPasteSaving(true);
      try {
        const uploads = await buildConvertedAuthJsonUploads(type, fileName, jsonText);
        const result = await uploadAuthJsonBatches(uploads);
        if (result.failed.length > 0) {
          const details = result.failed.map((item) => `${item.name}: ${item.error}`).join('; ');
          throw new Error(`${t('notification.save_failed')}: ${details}`);
        }
        const resolvedFileName = result.uploadedFiles[0] ?? uploads[0]?.name ?? fileName;
        try {
          await loadFiles({ throwOnError: true });
        } catch (reloadError) {
          const reloadMessage =
            reloadError instanceof Error ? reloadError.message : t('notification.refresh_failed');
          showNotification(
            uploads.length > 1
              ? t('auth_files.paste_success_batch', { count: uploads.length })
              : t('auth_files.paste_success', { name: resolvedFileName }),
            'success'
          );
          showNotification(`${t('notification.refresh_failed')}: ${reloadMessage}`, 'warning');
          return resolvedFileName;
        }
        showNotification(
          uploads.length > 1
            ? t('auth_files.paste_success_batch', { count: uploads.length })
            : t('auth_files.paste_success', { name: resolvedFileName }),
          'success'
        );
        return resolvedFileName;
      } catch (err) {
        throw new Error(err instanceof Error ? err.message : t('notification.save_failed'));
      } finally {
        authJsonPasteSavingRef.current = false;
        setAuthJsonPasteSaving(false);
      }
    },
    [loadFiles, showNotification, t, uploadAuthJsonBatches]
  );

  const handleDelete = useCallback(
    (name: string) => {
      showConfirmation({
        title: t('auth_files.delete_title', { defaultValue: 'Delete File' }),
        message: `${t('auth_files.delete_confirm')} "${name}" ?`,
        variant: 'danger',
        confirmText: t('common.confirm'),
        onConfirm: async () => {
          setDeleting(name);
          try {
            const result = await authFilesApi.deleteFile(name);
            showNotification(t('auth_files.delete_success'), 'success');
            applyDeletedFiles(result.files.length > 0 ? result.files : [name]);
          } catch (err: unknown) {
            const errorMessage = err instanceof Error ? err.message : '';
            showNotification(`${t('notification.delete_failed')}: ${errorMessage}`, 'error');
          } finally {
            setDeleting(null);
          }
        },
      });
    },
    [applyDeletedFiles, showConfirmation, showNotification, t]
  );

  const handleDeleteAll = useCallback(
    (deleteAllOptions: DeleteAllOptions) => {
      const {
        filter,
        problemOnly,
        disabledOnly,
        healthyOnly,
        filteredFiles,
        onResetFilterToAll,
        onResetProblemOnly,
        onResetDisabledOnly,
        onResetHealthyOnly,
        onResetResultFilters,
      } = deleteAllOptions;
      const normalizedFilter = normalizeProviderKey(filter);
      const isFiltered = normalizedFilter !== 'all';
      const isProblemOnly = problemOnly === true;
      const isDisabledOnly = disabledOnly === true;
      const isHealthyOnly = healthyOnly === true;
      const usesProvidedFilteredFiles = Array.isArray(filteredFiles);
      const isFilteredResult = usesProvidedFilteredFiles || isDisabledOnly || isHealthyOnly;
      const typeLabel = isFiltered ? getTypeLabel(t, normalizedFilter) : t('auth_files.filter_all');
      let confirmMessage = t('auth_files.delete_all_confirm');
      if (isFilteredResult) {
        confirmMessage = t('auth_files.delete_filtered_result_confirm_file_scope');
      } else if (isProblemOnly) {
        confirmMessage = isFiltered
          ? t('auth_files.delete_problem_filtered_confirm', { type: typeLabel })
          : t('auth_files.delete_problem_confirm');
      } else if (isFiltered) {
        confirmMessage = t('auth_files.delete_filtered_confirm', { type: typeLabel });
      }

      showConfirmation({
        title: t('auth_files.delete_all_title', { defaultValue: 'Delete All Files' }),
        message: confirmMessage,
        variant: 'danger',
        confirmText: t('common.confirm'),
        onConfirm: async () => {
          setDeletingAll(true);
          try {
            if (
              !isFiltered &&
              !isProblemOnly &&
              !isDisabledOnly &&
              !isHealthyOnly &&
              !usesProvidedFilteredFiles
            ) {
              await authFilesApi.deleteAll();
              showNotification(t('auth_files.delete_all_success'), 'success');
              setFiles((prev) => prev.filter((file) => isRuntimeOnlyAuthFile(file)));
              deselectAll();
            } else {
              const filesToDelete = (
                usesProvidedFilteredFiles
                  ? filteredFiles
                  : files.filter((file) => {
                      if (
                        isFiltered &&
                        normalizeProviderKey(String(file.type ?? file.provider ?? '')) !==
                          normalizedFilter
                      ) {
                        return false;
                      }
                      if (isProblemOnly && !hasAuthFileStatusMessage(file)) return false;
                      if (isDisabledOnly && file.disabled !== true) return false;
                      if (isHealthyOnly && !isHealthyAuthFile(file)) return false;
                      return true;
                    })
              ).filter((file) => !isRuntimeOnlyAuthFile(file));

              if (filesToDelete.length === 0) {
                let emptyMessage = t('auth_files.delete_filtered_none', { type: typeLabel });
                if (isFilteredResult) {
                  emptyMessage = t('auth_files.delete_filtered_result_none');
                } else if (isProblemOnly) {
                  emptyMessage = isFiltered
                    ? t('auth_files.delete_problem_filtered_none', { type: typeLabel })
                    : t('auth_files.delete_problem_none');
                }
                showNotification(emptyMessage, 'info');
                setDeletingAll(false);
                return;
              }

              const result = await authFilesApi.deleteFiles(filesToDelete.map((file) => file.name));
              const success = result.deleted;
              const failed = result.failed.length;

              applyDeletedFiles(result.files);

              if (failed === 0 && isFilteredResult) {
                showNotification(
                  t('auth_files.delete_filtered_result_success', { count: success }),
                  'success'
                );
              } else if (failed === 0 && isProblemOnly) {
                showNotification(
                  isFiltered
                    ? t('auth_files.delete_problem_filtered_success', {
                        count: success,
                        type: typeLabel,
                      })
                    : t('auth_files.delete_problem_success', { count: success }),
                  'success'
                );
              } else if (failed === 0) {
                showNotification(
                  t('auth_files.delete_filtered_success', { count: success, type: typeLabel }),
                  'success'
                );
              } else if (isFilteredResult) {
                showNotification(
                  t('auth_files.delete_filtered_result_partial', { success, failed }),
                  'warning'
                );
              } else if (isProblemOnly) {
                showNotification(
                  isFiltered
                    ? t('auth_files.delete_problem_filtered_partial', {
                        success,
                        failed,
                        type: typeLabel,
                      })
                    : t('auth_files.delete_problem_partial', { success, failed }),
                  'warning'
                );
              } else {
                showNotification(
                  t('auth_files.delete_filtered_partial', { success, failed, type: typeLabel }),
                  'warning'
                );
              }

              if (isFiltered) {
                onResetFilterToAll();
              }
              if (isProblemOnly) {
                onResetProblemOnly();
              }
              if (isDisabledOnly) {
                onResetDisabledOnly();
              }
              if (isHealthyOnly) {
                onResetHealthyOnly();
              }
              if (usesProvidedFilteredFiles) {
                onResetResultFilters?.();
              }
            }
          } catch (err: unknown) {
            const errorMessage = err instanceof Error ? err.message : '';
            showNotification(`${t('notification.delete_failed')}: ${errorMessage}`, 'error');
          } finally {
            setDeletingAll(false);
          }
        },
      });
    },
    [applyDeletedFiles, deselectAll, files, showConfirmation, showNotification, t]
  );

  const handleDownload = useCallback(
    async (name: string) => {
      try {
        const response = await apiClient.getRaw(
          `/auth-files/download?name=${encodeURIComponent(name)}`,
          { responseType: 'blob' }
        );
        const blob = new Blob([response.data]);
        downloadBlob({ filename: name, blob });
        showNotification(t('auth_files.download_success'), 'success');
      } catch (err: unknown) {
        const errorMessage = err instanceof Error ? err.message : '';
        showNotification(`${t('notification.download_failed')}: ${errorMessage}`, 'error');
      }
    },
    [showNotification, t]
  );

  const handleStatusToggle = useCallback(
    async (item: AuthFileItem, enabled: boolean) => {
      const name = item.name;
      const selectionKey = getAuthFileSelectionKey(item);
      const authIndex = normalizePatchTargetAuthIndex(
        (item['auth_index'] ?? item.authIndex) as AuthFilePatchTarget['authIndex']
      );
      const nextDisabled = !enabled;
      const previousDisabled = item.disabled === true;

      setStatusUpdating((prev) => ({ ...prev, [name]: true }));
      setFiles((prev) =>
        prev.map((file) =>
          getAuthFileSelectionKey(file) === selectionKey
            ? { ...file, disabled: nextDisabled }
            : file
        )
      );

      try {
        const res = await authFilesApi.setStatus(name, nextDisabled, authIndex);
        setFiles((prev) =>
          prev.map((file) =>
            getAuthFileSelectionKey(file) === selectionKey
              ? { ...file, disabled: res.disabled }
              : file
          )
        );
        showNotification(
          enabled
            ? t('auth_files.status_enabled_success', { name })
            : t('auth_files.status_disabled_success', { name }),
          'success'
        );
      } catch (err: unknown) {
        const errorMessage = err instanceof Error ? err.message : '';
        setFiles((prev) =>
          prev.map((file) =>
            getAuthFileSelectionKey(file) === selectionKey
              ? { ...file, disabled: previousDisabled }
              : file
          )
        );
        showNotification(`${t('notification.update_failed')}: ${errorMessage}`, 'error');
      } finally {
        setStatusUpdating((prev) => {
          if (!prev[name]) return prev;
          const next = { ...prev };
          delete next[name];
          return next;
        });
      }
    },
    [showNotification, t]
  );

  const batchSetStatus = useCallback(
    async (names: string[], enabled: boolean) => {
      if (batchStatusPendingRef.current) return;

      const uniqueNames = Array.from(new Set(names));
      if (uniqueNames.length === 0) return;
      if (uniqueNames.some((name) => statusUpdating[name] === true)) return;

      const originalDisabled = new Map(
        files
          .filter((file) => uniqueNames.includes(file.name))
          .map((file) => [file.name, file.disabled === true])
      );
      const targetNames = new Set(originalDisabled.keys());
      const targetNameList = Array.from(targetNames);
      if (targetNameList.length === 0) return;

      const nextDisabled = !enabled;

      batchStatusPendingRef.current = true;
      setBatchStatusUpdating(true);
      setStatusUpdating((prev) => {
        const next = { ...prev };
        targetNameList.forEach((name) => {
          next[name] = true;
        });
        return next;
      });
      setFiles((prev) =>
        prev.map((file) =>
          targetNames.has(file.name) ? { ...file, disabled: nextDisabled } : file
        )
      );

      try {
        const result = await authFilesApi.setStatuses(
          targetNameList.map((name) => ({ name })),
          nextDisabled
        );

        const successCount = result.updated;
        const failCount = result.failed.length;
        const failedNames = new Set(result.failed.map((item) => item.name));
        const confirmedDisabled = new Map(
          result.items.map((item) => [item.name, item.disabled] as const)
        );

        setFiles((prev) =>
          prev.map((file) => {
            if (failedNames.has(file.name)) {
              return { ...file, disabled: originalDisabled.get(file.name) === true };
            }
            if (confirmedDisabled.has(file.name)) {
              return { ...file, disabled: confirmedDisabled.get(file.name) };
            }
            return file;
          })
        );

        if (failCount === 0) {
          showNotification(
            t('auth_files.batch_status_success', { count: successCount }),
            'success'
          );
        } else {
          showNotification(
            t('auth_files.batch_status_partial', { success: successCount, failed: failCount }),
            'warning'
          );
        }

        deselectAll();
      } catch {
        setFiles((prev) =>
          prev.map((file) =>
            targetNames.has(file.name)
              ? { ...file, disabled: originalDisabled.get(file.name) === true }
              : file
          )
        );
        showNotification(
          t('auth_files.batch_status_partial', { success: 0, failed: targetNameList.length }),
          'warning'
        );
      } finally {
        batchStatusPendingRef.current = false;
        setBatchStatusUpdating(false);
        setStatusUpdating((prev) => {
          const next = { ...prev };
          targetNameList.forEach((name) => {
            delete next[name];
          });
          return next;
        });
      }
    },
    [deselectAll, files, showNotification, statusUpdating, t]
  );

  const batchPatchFields = useCallback(
    async (
      targets: AuthFilePatchTarget[],
      fields: AuthFileFieldsPatch
    ): Promise<AuthFilesBatchPatchResult | null> => {
      if (batchFieldsPendingRef.current) return null;

      const normalizedTargets = normalizeBatchPatchTargets(targets);
      if (normalizedTargets.length === 0) return null;
      if (Object.keys(fields).length === 0) return null;

      const groups = groupBatchPatchTargets(normalizedTargets);
      batchFieldsPendingRef.current = true;
      setBatchFieldsUpdating(true);

      try {
        const result = await authFilesApi.patchFieldsBatch(
          groups.map((group) => ({
            name: group.name,
            ...(group.authIndexes.length > 0 && group.authIndexes.length === group.targets.length
              ? { authIndexes: group.authIndexes }
              : {}),
          })),
          fields
        );

        const success = result.updated;
        const failed = result.failed.length;
        const failedNames = Array.from(new Set(result.failed.map((item) => item.name)));

        if (success > 0) {
          try {
            await loadFiles({ throwOnError: true });
          } catch (err: unknown) {
            const errorMessage =
              err instanceof Error ? err.message : t('notification.refresh_failed');
            showNotification(`${t('notification.refresh_failed')}: ${errorMessage}`, 'warning');
          }
        }

        if (failed === 0) {
          showNotification(t('auth_files.batch_fields_success', { count: success }), 'success');
        } else {
          showNotification(t('auth_files.batch_fields_partial', { success, failed }), 'warning');
        }

        deselectAll();
        return { success, failed, failedNames };
      } finally {
        batchFieldsPendingRef.current = false;
        setBatchFieldsUpdating(false);
      }
    },
    [deselectAll, loadFiles, showNotification, t]
  );

  const batchDownload = useCallback(
    async (names: string[]) => {
      const uniqueNames = Array.from(new Set(names));
      if (uniqueNames.length === 0) return;

      let successCount = 0;
      let failCount = uniqueNames.length;
      try {
        const result = await authFilesApi.downloadFiles(uniqueNames);
        successCount = result.included;
        failCount = result.failed;
        if (result.included > 0) {
          downloadBlob({ filename: result.filename, blob: result.blob });
        }
      } catch {
        successCount = 0;
        failCount = uniqueNames.length;
      }

      if (failCount === 0) {
        showNotification(
          t('auth_files.batch_download_success', { count: successCount }),
          'success'
        );
      } else {
        showNotification(
          t('auth_files.batch_download_partial', { success: successCount, failed: failCount }),
          'warning'
        );
      }
    },
    [showNotification, t]
  );

  const batchDelete = useCallback(
    (names: string[]) => {
      const uniqueNames = Array.from(new Set(names));
      if (uniqueNames.length === 0) return;

      showConfirmation({
        title: t('auth_files.batch_delete_title'),
        message: t('auth_files.batch_delete_confirm', { count: uniqueNames.length }),
        variant: 'danger',
        confirmText: t('common.confirm'),
        onConfirm: async () => {
          try {
            const result = await authFilesApi.deleteFiles(uniqueNames);
            applyDeletedFiles(result.files);

            if (result.failed.length === 0) {
              showNotification(
                `${t('auth_files.delete_all_success')} (${result.deleted})`,
                'success'
              );
            } else {
              showNotification(
                t('auth_files.delete_filtered_partial', {
                  success: result.deleted,
                  failed: result.failed.length,
                  type: t('auth_files.filter_all'),
                }),
                'warning'
              );
            }
          } catch (err: unknown) {
            const errorMessage = err instanceof Error ? err.message : '';
            showNotification(`${t('notification.delete_failed')}: ${errorMessage}`, 'error');
          }
        },
      });
    },
    [applyDeletedFiles, showConfirmation, showNotification, t]
  );

  return {
    files: syncedFiles,
    total,
    providerFacets,
    serverPaged,
    selectedFiles,
    selectionCount,
    loading,
    error,
    uploading,
    authJsonPasteSaving,
    deleting,
    deletingAll,
    statusUpdating,
    batchStatusUpdating,
    batchFieldsUpdating,
    fileInputRef,
    loadFiles,
    prepareAuthJsonConverter,
    handleUploadClick,
    handleFileChange,
    savePastedAuthJson,
    handleDelete,
    handleDeleteAll,
    handleDownload,
    handleStatusToggle,
    toggleSelect,
    selectAllVisible,
    invertVisibleSelection,
    deselectAll,
    batchDownload,
    batchSetStatus,
    batchPatchFields,
    batchDelete,
  };
}
