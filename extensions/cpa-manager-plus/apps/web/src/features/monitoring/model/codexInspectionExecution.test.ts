import { beforeEach, describe, expect, it, vi } from 'vitest';
import { authFilesApi } from '@/services/api/authFiles';
import { usageServiceApi } from '@/services/api/usageService';
import type { CodexInspectionResultItem, CodexInspectionSettings } from '../codexInspection';
import { executeCodexInspectionActions } from './codexInspectionExecution';

vi.mock('@/services/api/authFiles', () => ({
  authFilesApi: {
    setStatusWithFallback: vi.fn(),
    deleteFileByName: vi.fn(),
    listSummary: vi.fn(),
  },
}));

vi.mock('@/services/api/usageService', () => ({
  usageServiceApi: {
    disableCodexInspectionUntilReset: vi.fn(),
  },
}));

const mockedAuthFilesApi = vi.mocked(authFilesApi);
const mockedUsageServiceApi = vi.mocked(usageServiceApi);

const settings: CodexInspectionSettings = {
  baseUrl: '',
  token: '',
  targetType: 'codex',
  workers: 1,
  deleteWorkers: 1,
  timeout: 15_000,
  retries: 0,
  userAgent: 'test',
  usedPercentThreshold: 100,
  sampleSize: 0,
};

const createDisableItem = (
  quotaWindows: CodexInspectionResultItem['quotaWindows']
): CodexInspectionResultItem => ({
  key: 'auth-a.json::auth-1',
  fileName: 'auth-a.json',
  displayAccount: 'alice@example.com',
  authIndex: 'auth-1',
  accountId: null,
  provider: 'codex',
  disabled: false,
  status: 'ok',
  state: 'ready',
  raw: { name: 'auth-a.json', type: 'codex', auth_index: 'auth-1' },
  action: 'disable',
  actionReason: 'temporary quota cooldown',
  statusCode: 200,
  usedPercent: 100,
  isQuota: true,
  error: '',
  quotaWindows,
});

describe('executeCodexInspectionActions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockedAuthFilesApi.listSummary.mockResolvedValue({ files: [] } as never);
    mockedAuthFilesApi.setStatusWithFallback.mockResolvedValue(undefined as never);
    mockedUsageServiceApi.disableCodexInspectionUntilReset.mockResolvedValue({
      ok: true,
      recoverAtMs: Date.now() + 3_600_000,
    });
  });

  it('registers a five-hour cooldown instead of directly disabling the file', async () => {
    const recoverAtMs = Date.now() + 3_600_000;
    const item = createDisableItem([
      {
        id: 'five-hour',
        labelKey: 'codex_quota.primary_window',
        usedPercent: 100,
        resetLabel: '07/14 23:00',
        resetAtMs: recoverAtMs,
        cooldownRecommended: true,
        limitWindowSeconds: 18_000,
      },
    ]);

    const result = await executeCodexInspectionActions({
      settings,
      items: [item],
      previousFiles: [],
      apiBase: 'http://manager.local',
      managementKey: 'management-key',
    });

    expect(mockedUsageServiceApi.disableCodexInspectionUntilReset).toHaveBeenCalledWith(
      'http://manager.local',
      'management-key',
      {
        fileName: 'auth-a.json',
        authIndex: 'auth-1',
        displayAccount: 'alice@example.com',
        recoverAtMs,
      }
    );
    expect(mockedAuthFilesApi.setStatusWithFallback).not.toHaveBeenCalled();
    expect(result.outcomes[0]).toMatchObject({ action: 'disable', success: true });
  });

  it('keeps the direct status path for non-cooldown disable suggestions', async () => {
    const item = createDisableItem([]);

    await executeCodexInspectionActions({
      settings,
      items: [item],
      previousFiles: [],
      apiBase: 'http://manager.local',
      managementKey: 'management-key',
    });

    expect(mockedAuthFilesApi.setStatusWithFallback).toHaveBeenCalledWith('auth-a.json', true);
    expect(mockedUsageServiceApi.disableCodexInspectionUntilReset).not.toHaveBeenCalled();
  });

  it('does not permanently disable an expired cooldown recommendation', async () => {
    const item = createDisableItem([
      {
        id: 'five-hour',
        labelKey: 'codex_quota.primary_window',
        usedPercent: 100,
        resetLabel: 'expired',
        resetAtMs: Date.now() - 1_000,
        cooldownRecommended: true,
        limitWindowSeconds: 18_000,
      },
    ]);

    const result = await executeCodexInspectionActions({
      settings,
      items: [item],
      previousFiles: [],
      apiBase: 'http://manager.local',
      managementKey: 'management-key',
    });

    expect(result.outcomes[0]).toMatchObject({ action: 'disable', success: false });
    expect(result.outcomes[0].error).toContain('重置时间已过期');
    expect(mockedAuthFilesApi.setStatusWithFallback).not.toHaveBeenCalled();
    expect(mockedUsageServiceApi.disableCodexInspectionUntilReset).not.toHaveBeenCalled();
  });
});
