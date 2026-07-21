import { act, createElement } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => {
  return {
    mocks: {
      list: vi.fn(),
      uploadJsonTexts: vi.fn(),
      deleteFiles: vi.fn(),
      patchFields: vi.fn(),
      patchFieldsForAuthIndexes: vi.fn(),
      patchFieldsBatch: vi.fn(),
      showNotification: vi.fn(),
      showConfirmation: vi.fn(),
    },
  };
});

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) => {
      if (options && typeof options.name === 'string') {
        return `${key}:${options.name}`;
      }
      return key;
    },
  }),
}));

vi.mock('@/stores', () => ({
  useNotificationStore: () => ({
    showNotification: mocks.showNotification,
    showConfirmation: mocks.showConfirmation,
  }),
}));

vi.mock('@/services/api', () => ({
  authFilesApi: {
    list: mocks.list,
    listSummary: mocks.list,
    uploadJsonTexts: mocks.uploadJsonTexts,
    deleteFiles: mocks.deleteFiles,
    patchFields: mocks.patchFields,
    patchFieldsForAuthIndexes: mocks.patchFieldsForAuthIndexes,
    patchFieldsBatch: mocks.patchFieldsBatch,
  },
}));

import { buildConvertedAuthJsonUploads, useAuthFilesData } from './useAuthFilesData';

type UseAuthFilesDataHarness = {
  getCurrent: () => ReturnType<typeof useAuthFilesData>;
  getSavingHistory: () => boolean[];
  unmount: () => void;
};

const mountUseAuthFilesData = (): UseAuthFilesDataHarness => {
  let hook: ReturnType<typeof useAuthFilesData> | null = null;
  let lastSavingState: boolean | undefined;
  const savingHistory: boolean[] = [];
  let renderer: ReactTestRenderer | null = null;

  const captureHook = (value: ReturnType<typeof useAuthFilesData>) => {
    hook = value;
    if (value.authJsonPasteSaving !== lastSavingState) {
      lastSavingState = value.authJsonPasteSaving;
      savingHistory.push(value.authJsonPasteSaving);
    }
  };

  function HookHarness() {
    captureHook(useAuthFilesData());
    return null;
  }

  act(() => {
    renderer = create(createElement(HookHarness));
  });

  return {
    getCurrent: () => {
      if (!hook) {
        throw new Error('Failed to mount useAuthFilesData test harness');
      }
      return hook;
    },
    getSavingHistory: () => [...savingHistory],
    unmount: () => {
      if (!renderer) return;
      act(() => {
        renderer?.unmount();
      });
    },
  };
};

beforeEach(() => {
  mocks.list.mockReset();
  mocks.uploadJsonTexts.mockReset();
  mocks.deleteFiles.mockReset();
  mocks.patchFields.mockReset();
  mocks.patchFieldsForAuthIndexes.mockReset();
  mocks.patchFieldsBatch.mockReset();
  mocks.showNotification.mockReset();
  mocks.showConfirmation.mockReset();

  mocks.list.mockResolvedValue({ files: [] });
  mocks.uploadJsonTexts.mockImplementation(async (files: Array<{ name: string }>) => ({
    status: 'ok',
    uploaded: files.length,
    files: files.map((file) => file.name),
    failed: [],
  }));
  mocks.deleteFiles.mockResolvedValue({ deleted: 0, failed: [], files: [] });
  mocks.patchFields.mockResolvedValue(undefined);
  mocks.patchFieldsForAuthIndexes.mockResolvedValue(undefined);
  mocks.patchFieldsBatch.mockResolvedValue({ status: 'ok', updated: 0, failed: [] });
});

describe('buildConvertedAuthJsonUploads', () => {
  it('keeps explicit file names for pasted CPA auth JSON', async () => {
    const input = {
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    };

    const result = await buildConvertedAuthJsonUploads(
      'cpa',
      'custom-auth.json',
      JSON.stringify(input)
    );

    expect(result).toEqual([{ name: 'custom-auth.json', text: JSON.stringify(input) }]);
  });

  it('keeps explicit file names for pasted session auth JSON when a custom name is provided', async () => {
    const result = await buildConvertedAuthJsonUploads(
      'session',
      'my-work-account.json',
      JSON.stringify({
        user: { email: 'Session.User+tag@example.com' },
        account: { id: 'session-account' },
        accessToken: 'plain-access-token',
      })
    );

    expect(result[0].name).toBe('my-work-account.json');
  });

  it('derives a default codex file name for pasted session auth JSON', async () => {
    const result = await buildConvertedAuthJsonUploads(
      'session',
      'codex-account.json',
      JSON.stringify({
        user: { email: 'Session.User+tag@example.com' },
        account: { id: 'session-account' },
        accessToken: 'plain-access-token',
      })
    );

    expect(result[0].name).toBe('codex-session-session.user+tag@example.com.json');
    expect(JSON.parse(result[0].text)).toMatchObject({
      type: 'codex',
      email: 'Session.User+tag@example.com',
      account_id: 'session-account',
      access_token: 'plain-access-token',
    });
  });

  it('derives a default file name for multi-account sub2api auth JSON', async () => {
    const result = await buildConvertedAuthJsonUploads(
      'sub2api',
      'codex-account.json',
      JSON.stringify({
        exported_at: '2026-06-01T12:00:00.000Z',
        proxies: [],
        accounts: [
          {
            name: 'First OpenAI',
            platform: 'openai',
            type: 'oauth',
            credentials: {
              access_token: 'first-access-token',
              email: 'first@example.com',
            },
          },
          {
            name: 'Second OpenAI',
            platform: 'openai',
            type: 'oauth',
            credentials: {
              access_token: 'second-access-token',
              email: 'second@example.com',
            },
          },
        ],
      })
    );

    expect(result).toHaveLength(2);
    expect(result.map((item) => JSON.parse(item.text))).toEqual([
      expect.objectContaining({
        type: 'codex',
        email: 'first@example.com',
        access_token: 'first-access-token',
      }),
      expect.objectContaining({
        type: 'codex',
        email: 'second@example.com',
        access_token: 'second-access-token',
      }),
    ]);
  });
});

describe('useAuthFilesData savePastedAuthJson', () => {
  it('saves converted session JSON with derived default file name and reloads files', async () => {
    const hook = mountUseAuthFilesData();
    const sessionInput = JSON.stringify({
      user: { email: 'Session.User+tag@example.com' },
      account: { id: 'session-account' },
      accessToken: 'plain-access-token',
    });

    const savedName = await hook
      .getCurrent()
      .savePastedAuthJson('session', 'codex-account.json', sessionInput);

    expect(savedName).toBe('codex-session-session.user+tag@example.com.json');
    expect(mocks.uploadJsonTexts).toHaveBeenCalledWith([
      expect.objectContaining({
        name: 'codex-session-session.user+tag@example.com.json',
        text: expect.any(String),
      }),
    ]);
    expect(JSON.parse(mocks.uploadJsonTexts.mock.calls[0][0][0].text)).toMatchObject({
      type: 'codex',
      email: 'Session.User+tag@example.com',
      account_id: 'session-account',
      access_token: 'plain-access-token',
    });
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.paste_success:codex-session-session.user+tag@example.com.json',
      'success'
    );
    expect(mocks.list).toHaveBeenCalledTimes(1);
    hook.unmount();
  });

  it('saves CPA JSON unchanged with explicit file name', async () => {
    const hook = mountUseAuthFilesData();
    const cpaInput = {
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    };

    const savedName = await hook
      .getCurrent()
      .savePastedAuthJson('cpa', 'custom-auth.json', JSON.stringify(cpaInput));

    expect(savedName).toBe('custom-auth.json');
    expect(mocks.uploadJsonTexts).toHaveBeenCalledWith([
      { name: 'custom-auth.json', text: JSON.stringify(cpaInput) },
    ]);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    hook.unmount();
  });

  it('splits converted sub2api JSON into separate CPA auth files', async () => {
    const hook = mountUseAuthFilesData();
    const sub2apiInput = JSON.stringify({
      exported_at: '2026-06-01T12:00:00.000Z',
      proxies: [],
      accounts: [
        {
          name: 'First OpenAI',
          platform: 'openai',
          type: 'oauth',
          credentials: {
            access_token: 'first-access-token',
            email: 'first@example.com',
          },
        },
        {
          name: 'Second OpenAI',
          platform: 'openai',
          type: 'oauth',
          credentials: {
            access_token: 'second-access-token',
            email: 'second@example.com',
          },
        },
      ],
    });

    const savedName = await hook
      .getCurrent()
      .savePastedAuthJson('sub2api', 'codex-account.json', sub2apiInput);

    expect(savedName).toContain('first@example.com');
    const uploads = mocks.uploadJsonTexts.mock.calls[0][0];
    expect(uploads).toHaveLength(2);
    expect(uploads.map((item: { text: string }) => JSON.parse(item.text))).toEqual([
      expect.objectContaining({
        type: 'codex',
        email: 'first@example.com',
        access_token: 'first-access-token',
      }),
      expect.objectContaining({
        type: 'codex',
        email: 'second@example.com',
        access_token: 'second-access-token',
      }),
    ]);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    hook.unmount();
  });

  it('uploads 50 converted accounts in 20/20/10 batches', async () => {
    const hook = mountUseAuthFilesData();
    const accounts = Array.from({ length: 50 }, (_, index) => ({
      type: 'codex',
      email: `user-${index + 1}@example.com`,
      access_token: `access-token-${index + 1}`,
    }));

    await hook
      .getCurrent()
      .savePastedAuthJson('cpa', 'bulk-accounts.json', JSON.stringify(accounts));

    expect(mocks.uploadJsonTexts).toHaveBeenCalledTimes(3);
    expect(mocks.uploadJsonTexts.mock.calls[0][0]).toHaveLength(20);
    expect(mocks.uploadJsonTexts.mock.calls[1][0]).toHaveLength(20);
    expect(mocks.uploadJsonTexts.mock.calls[2][0]).toHaveLength(10);
    expect(mocks.uploadJsonTexts.mock.calls[0][0][0].name).toBe('bulk-accounts-001.json');
    expect(mocks.uploadJsonTexts.mock.calls[2][0][9].name).toBe('bulk-accounts-050.json');
    hook.unmount();
  });

  it('waits for file reload completion before resolving pasted save success', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    let resolveList: (() => void) | undefined;
    mocks.list.mockImplementationOnce(
      () =>
        new Promise<{ files: [] }>((resolve) => {
          resolveList = () => resolve({ files: [] });
        })
    );

    const settled = vi.fn();
    const savePromise = hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput);
    void savePromise.then(settled);

    await vi.waitFor(() => expect(resolveList).toBeTypeOf('function'));

    expect(settled).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalled();

    resolveList?.();
    await savePromise;
    expect(settled).toHaveBeenCalledWith('custom-auth.json');
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.paste_success:custom-auth.json',
      'success'
    );
    hook.unmount();
  });

  it('sets authJsonPasteSaving true during save and resets false after success', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    let resolveUpload: (() => void) | undefined;
    mocks.uploadJsonTexts.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveUpload = () =>
            resolve({ status: 'ok', uploaded: 1, files: ['custom-auth.json'], failed: [] });
        })
    );

    const savePromise = hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput);
    await act(async () => {
      await Promise.resolve();
    });
    expect(hook.getCurrent().authJsonPasteSaving).toBe(true);

    expect(resolveUpload).toBeTypeOf('function');
    resolveUpload?.();
    await expect(savePromise).resolves.toBe('custom-auth.json');
    await act(async () => {
      await Promise.resolve();
    });

    expect(hook.getCurrent().authJsonPasteSaving).toBe(false);
    const savingHistory = hook.getSavingHistory();
    expect(savingHistory).toContain(true);
    expect(savingHistory[savingHistory.length - 1]).toBe(false);
    hook.unmount();
  });

  it('rejects a concurrent pasted save before starting a duplicate upload', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    let resolveUpload: (() => void) | undefined;
    mocks.uploadJsonTexts.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveUpload = () =>
            resolve({ status: 'ok', uploaded: 1, files: ['custom-auth.json'], failed: [] });
        })
    );

    const firstSave = hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput);
    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput)
    ).rejects.toThrow('auth_files.paste_error_save_in_progress');

    expect(mocks.uploadJsonTexts).toHaveBeenCalledTimes(1);
    expect(resolveUpload).toBeTypeOf('function');
    resolveUpload?.();
    await expect(firstSave).resolves.toBe('custom-auth.json');
    hook.unmount();
  });

  it('throws on invalid conversion and does not upload or show success notification', async () => {
    const hook = mountUseAuthFilesData();
    const invalidInput = JSON.stringify({ foo: 'bar' });

    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', invalidInput)
    ).rejects.toThrow();

    expect(mocks.uploadJsonTexts).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalled();
    expect(mocks.list).not.toHaveBeenCalled();
    hook.unmount();
  });

  it('throws a generic save failure on upload failure and does not show success notification or reload files', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    mocks.uploadJsonTexts.mockRejectedValueOnce(
      new Error('upload failed for token sk-secret-value')
    );

    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput)
    ).rejects.toThrow('notification.save_failed');

    expect(mocks.showNotification).not.toHaveBeenCalled();
    expect(mocks.list).not.toHaveBeenCalled();
    hook.unmount();
  });

  it('resolves saved file name when reload fails after upload and shows refresh warning', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    mocks.list.mockClear();
    mocks.list.mockRejectedValueOnce(new Error('reload failed'));

    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput)
    ).resolves.toBe('custom-auth.json');

    expect(mocks.uploadJsonTexts).toHaveBeenCalledTimes(1);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.paste_success:custom-auth.json',
      'success'
    );
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'notification.refresh_failed: reload failed',
      'warning'
    );
    hook.unmount();
  });

  it('sets authJsonPasteSaving true during save and resets false after failure', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    let rejectUpload: ((reason?: unknown) => void) | undefined;
    mocks.uploadJsonTexts.mockImplementationOnce(
      () =>
        new Promise<void>((_, reject) => {
          rejectUpload = reject;
        })
    );

    const savePromise = hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput);
    await act(async () => {
      await Promise.resolve();
    });
    expect(hook.getCurrent().authJsonPasteSaving).toBe(true);

    expect(rejectUpload).toBeTypeOf('function');
    rejectUpload?.(new Error('upload failed'));
    await expect(savePromise).rejects.toThrow('notification.save_failed');
    await act(async () => {
      await Promise.resolve();
    });

    expect(hook.getCurrent().authJsonPasteSaving).toBe(false);
    const savingHistory = hook.getSavingHistory();
    expect(savingHistory).toContain(true);
    expect(savingHistory[savingHistory.length - 1]).toBe(false);
    hook.unmount();
  });

  it('allows retrying pasted save after an upload failure', async () => {
    const hook = mountUseAuthFilesData();
    const validInput = JSON.stringify({
      type: 'codex',
      email: 'user@example.com',
      access_token: 'existing-access-token',
    });
    mocks.uploadJsonTexts.mockRejectedValueOnce(new Error('upload failed'));

    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput)
    ).rejects.toThrow('notification.save_failed');
    await expect(
      hook.getCurrent().savePastedAuthJson('cpa', 'custom-auth.json', validInput)
    ).resolves.toBe('custom-auth.json');

    expect(mocks.uploadJsonTexts).toHaveBeenCalledTimes(2);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.paste_success:custom-auth.json',
      'success'
    );
    hook.unmount();
  });
});

describe('useAuthFilesData handleDeleteAll', () => {
  it('deletes only the provided filtered files for custom result filters', async () => {
    const hook = mountUseAuthFilesData();
    const resetResultFilters = vi.fn();
    const resetFilterToAll = vi.fn();

    mocks.list.mockResolvedValueOnce({
      files: [
        { name: 'codex-limited.json', type: 'codex' },
        { name: 'codex-ok.json', type: 'codex' },
      ],
    });
    mocks.deleteFiles.mockResolvedValueOnce({
      deleted: 1,
      failed: [],
      files: ['codex-limited.json'],
    });

    await act(async () => {
      await hook.getCurrent().loadFiles();
    });

    act(() => {
      hook.getCurrent().handleDeleteAll({
        filter: 'all',
        problemOnly: false,
        disabledOnly: false,
        healthyOnly: false,
        filteredFiles: [{ name: 'codex-limited.json', type: 'codex' }],
        onResetFilterToAll: resetFilterToAll,
        onResetProblemOnly: vi.fn(),
        onResetDisabledOnly: vi.fn(),
        onResetHealthyOnly: vi.fn(),
        onResetResultFilters: resetResultFilters,
      });
    });

    const confirmation = mocks.showConfirmation.mock.calls[0]?.[0] as
      | { onConfirm?: () => Promise<void> }
      | undefined;
    expect(confirmation?.onConfirm).toBeTypeOf('function');
    expect(mocks.showConfirmation).toHaveBeenCalledWith(
      expect.objectContaining({
        message: 'auth_files.delete_filtered_result_confirm_file_scope',
      })
    );

    await act(async () => {
      await confirmation?.onConfirm?.();
    });

    expect(mocks.deleteFiles).toHaveBeenCalledWith(['codex-limited.json']);
    expect(resetFilterToAll).not.toHaveBeenCalled();
    expect(resetResultFilters).toHaveBeenCalledTimes(1);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.delete_filtered_result_success',
      'success'
    );
    hook.unmount();
  });
});

describe('useAuthFilesData batchPatchFields', () => {
  it('patches selected auth indexes from the same file in one request', async () => {
    const hook = mountUseAuthFilesData();
    mocks.patchFieldsBatch.mockResolvedValueOnce({ status: 'ok', updated: 2, failed: [] });

    let result: Awaited<ReturnType<ReturnType<typeof useAuthFilesData>['batchPatchFields']>> = null;
    await act(async () => {
      result = await hook.getCurrent().batchPatchFields(
        [
          { name: 'shared-codex.json', authIndex: 'auth-1' },
          { name: 'shared-codex.json', authIndex: 'auth-2' },
          { name: 'shared-codex.json', authIndex: 'auth-1' },
        ],
        { priority: 10 }
      );
    });

    expect(mocks.patchFieldsBatch).toHaveBeenCalledWith(
      [{ name: 'shared-codex.json', authIndexes: ['auth-1', 'auth-2'] }],
      { priority: 10 }
    );
    expect(mocks.patchFieldsForAuthIndexes).not.toHaveBeenCalled();
    expect(mocks.patchFields).not.toHaveBeenCalled();
    expect(result).toEqual({ success: 2, failed: 0, failedNames: [] });
    expect(mocks.list).toHaveBeenCalledTimes(1);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'auth_files.batch_fields_success',
      'success'
    );
    hook.unmount();
  });

  it('sends a file-level batch target when auth index is absent', async () => {
    const hook = mountUseAuthFilesData();
    mocks.patchFieldsBatch.mockResolvedValueOnce({ status: 'ok', updated: 1, failed: [] });

    let result: Awaited<ReturnType<ReturnType<typeof useAuthFilesData>['batchPatchFields']>> = null;
    await act(async () => {
      result = await hook
        .getCurrent()
        .batchPatchFields([{ name: 'single-codex.json' }], { websockets: false });
    });

    expect(mocks.patchFieldsBatch).toHaveBeenCalledWith([{ name: 'single-codex.json' }], {
      websockets: false,
    });
    expect(mocks.patchFields).not.toHaveBeenCalled();
    expect(mocks.patchFieldsForAuthIndexes).not.toHaveBeenCalled();
    expect(result).toEqual({ success: 1, failed: 0, failedNames: [] });
    hook.unmount();
  });
});
