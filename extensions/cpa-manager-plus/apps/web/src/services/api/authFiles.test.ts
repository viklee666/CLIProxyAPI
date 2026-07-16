import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
    getRaw: vi.fn(),
    requestRaw: vi.fn(),
    postForm: vi.fn(),
    patch: vi.fn(),
  },
}));

vi.mock('./client', () => ({
  apiClient: {
    get: mocks.get,
    getRaw: mocks.getRaw,
    requestRaw: mocks.requestRaw,
    postForm: mocks.postForm,
    patch: mocks.patch,
  },
}));

import { authFilesApi } from './authFiles';

beforeEach(() => {
  mocks.get.mockReset();
  mocks.getRaw.mockReset();
  mocks.requestRaw.mockReset();
  mocks.postForm.mockReset();
  mocks.patch.mockReset();
});

describe('authFilesApi OAuth model alias normalization', () => {
  it('merges paginated OAuth maps and model definitions', async () => {
    mocks.get
      .mockResolvedValueOnce({
        'oauth-model-alias': { codex: [{ name: 'source-1', alias: 'alias-1' }] },
        page: 1,
        page_size: 1,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        'oauth-model-alias': { codex: [{ name: 'source-2', alias: 'alias-2' }] },
        page: 2,
        page_size: 1,
        total_pages: 2,
        has_more: false,
      })
      .mockResolvedValueOnce({
        models: [{ id: 'model-1' }],
        page: 1,
        page_size: 1,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        models: [{ id: 'model-2' }],
        page: 2,
        page_size: 1,
        total_pages: 2,
        has_more: false,
      });

    await expect(authFilesApi.getOauthModelAlias()).resolves.toEqual({
      codex: [
        { name: 'source-1', alias: 'alias-1' },
        { name: 'source-2', alias: 'alias-2' },
      ],
    });
    await expect(authFilesApi.getModelDefinitions('codex')).resolves.toEqual([
      { id: 'model-1' },
      { id: 'model-2' },
    ]);
    expect(mocks.get).toHaveBeenNthCalledWith(2, '/oauth-model-alias', {
      params: { page: 2, page_size: 1 },
    });
    expect(mocks.get).toHaveBeenNthCalledWith(4, '/model-definitions/codex', {
      params: { page: 2, page_size: 1 },
    });
  });

  it('preserves force-mapping returned by CPA', async () => {
    mocks.get.mockResolvedValue({
      'oauth-model-alias': {
        codex: [
          {
            name: 'gpt-5-codex',
            alias: 'team-codex',
            fork: true,
            'force-mapping': true,
          },
        ],
      },
    });

    await expect(authFilesApi.getOauthModelAlias()).resolves.toEqual({
      codex: [
        {
          name: 'gpt-5-codex',
          alias: 'team-codex',
          fork: true,
          forceMapping: true,
        },
      ],
    });
  });

  it('serializes forceMapping using the CPA force-mapping field', async () => {
    mocks.patch.mockResolvedValue({ status: 'ok' });

    await authFilesApi.saveOauthModelAlias('codex', [
      {
        name: 'gpt-5-codex',
        alias: 'team-codex',
        forceMapping: true,
      },
    ]);

    expect(mocks.patch).toHaveBeenCalledWith('/oauth-model-alias', {
      channel: 'codex',
      aliases: [
        {
          name: 'gpt-5-codex',
          alias: 'team-codex',
          'force-mapping': true,
        },
      ],
    });
  });
});

describe('authFilesApi list normalization', () => {
  it('preserves same-name auth file rows when authIndex differs', async () => {
    mocks.get.mockResolvedValue({
      files: [
        {
          name: 'sub2api-codex-accounts.codex.json',
          type: 'codex',
          authIndex: 1,
          account: 'second@example.com',
        },
        {
          name: 'sub2api-codex-accounts.codex.json',
          type: 'codex',
          authIndex: 0,
          account: 'first@example.com',
        },
      ],
    });

    const result = await authFilesApi.list();

    expect(mocks.get).toHaveBeenCalledWith('/auth-files');
    expect(result.files).toEqual([
      expect.objectContaining({
        name: 'sub2api-codex-accounts.codex.json',
        authIndex: 0,
        account: 'first@example.com',
      }),
      expect.objectContaining({
        name: 'sub2api-codex-accounts.codex.json',
        authIndex: 1,
        account: 'second@example.com',
      }),
    ]);
    expect(result.total).toBe(2);
  });

  it('still merges duplicate same-name rows when authIndex is absent', async () => {
    mocks.get.mockResolvedValue({
      files: [
        {
          name: 'single-codex.json',
          type: 'codex',
          source: 'runtime',
          status: 'ok',
        },
        {
          name: 'single-codex.json',
          type: 'codex',
          source: 'file',
          path: '/auth/single-codex.json',
          size: 123,
        },
      ],
    });

    const result = await authFilesApi.list();

    expect(result.files).toHaveLength(1);
    expect(result.files[0]).toEqual(
      expect.objectContaining({
        name: 'single-codex.json',
        source: 'file',
        path: '/auth/single-codex.json',
        size: 123,
        status: 'ok',
      })
    );
    expect(result.total).toBe(1);
  });
});

describe('authFilesApi save auth file upload contracts', () => {
  const getUploadedFile = () => {
    const formData = mocks.postForm.mock.calls[0]?.[1];
    expect(formData).toBeInstanceOf(FormData);
    const file = (formData as FormData).get('file');
    expect(file).toBeInstanceOf(File);
    return file as File;
  };

  it('saveText resolves when upload reports one uploaded file', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 1,
      files: ['direct-auth.json'],
      failed: [],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveText('direct-auth.json', '{"type":"codex","access_token":"token"}')
    ).resolves.toBeUndefined();
    expect(mocks.postForm).toHaveBeenCalledWith('/auth-files', expect.any(FormData));
    const file = getUploadedFile();
    expect(file.name).toBe('direct-auth.json');
    await expect(file.text()).resolves.toBe('{"type":"codex","access_token":"token"}');
  });

  it('saveJsonObject resolves when upload succeeds', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 1,
      files: ['converted-auth.json'],
      failed: [],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('converted-auth.json', {
        type: 'codex',
        access_token: 'token',
      })
    ).resolves.toBeUndefined();
    expect(mocks.postForm).toHaveBeenCalledWith('/auth-files', expect.any(FormData));
    const file = getUploadedFile();
    expect(file.name).toBe('converted-auth.json');
    await expect(file.text()).resolves.toBe('{"type":"codex","access_token":"token"}');
  });

  it('saveJsonObject serializes auth file arrays without wrapping them', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 1,
      files: ['converted-auth-array.json'],
      failed: [],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('converted-auth-array.json', [
        {
          type: 'codex',
          access_token: 'first-token',
        },
        {
          type: 'codex',
          access_token: 'second-token',
        },
      ])
    ).resolves.toBeUndefined();
    expect(mocks.postForm).toHaveBeenCalledWith('/auth-files', expect.any(FormData));
    const file = getUploadedFile();
    expect(file.name).toBe('converted-auth-array.json');
    await expect(file.text()).resolves.toBe(
      '[{"type":"codex","access_token":"first-token"},{"type":"codex","access_token":"second-token"}]'
    );
  });

  it('uploadFiles sends a multi-file selection in one multipart request', async () => {
    // Arrange
    mocks.postForm.mockResolvedValueOnce({
      status: 'ok',
      uploaded: 2,
      files: ['first-auth.json', 'second-auth.json'],
      failed: [],
    });

    const firstFile = new File(['{"type":"codex"}'], 'first-auth.json', {
      type: 'application/json',
    });
    const secondFile = new File(['{"type":"claude"}'], 'second-auth.json', {
      type: 'application/json',
    });

    // Act
    const result = await authFilesApi.uploadFiles([firstFile, secondFile]);

    // Assert
    expect(result).toEqual({
      status: 'ok',
      uploaded: 2,
      files: ['first-auth.json', 'second-auth.json'],
      failed: [],
    });
    expect(mocks.postForm).toHaveBeenCalledTimes(1);

    const formData = mocks.postForm.mock.calls[0]?.[1] as FormData;
    const uploadedFiles = formData.getAll('files') as File[];
    expect(uploadedFiles.map((file) => file.name)).toEqual(['first-auth.json', 'second-auth.json']);
  });

  it('uploadJsonTexts creates named JSON blobs for converted CPA records', async () => {
    mocks.postForm.mockResolvedValueOnce({
      status: 'ok',
      uploaded: 2,
      files: ['first-cpa.json', 'second-cpa.json'],
      failed: [],
    });

    const result = await authFilesApi.uploadJsonTexts([
      { name: 'first-cpa.json', text: '{"type":"codex","access_token":"first"}' },
      { name: 'second-cpa.json', text: '{"type":"codex","access_token":"second"}' },
    ]);

    expect(result.uploaded).toBe(2);
    const formData = mocks.postForm.mock.calls[0]?.[1] as FormData;
    const uploadedFiles = formData.getAll('files') as File[];
    expect(uploadedFiles.map((file) => file.name)).toEqual(['first-cpa.json', 'second-cpa.json']);
    await expect(uploadedFiles[0].text()).resolves.toContain('"access_token":"first"');
  });

  it('uploadFiles preserves partial failures returned by the batch endpoint', async () => {
    // Arrange
    mocks.postForm.mockResolvedValueOnce({
      status: 'partial',
      uploaded: 1,
      files: ['first-auth.json'],
      failed: [{ name: 'second-auth.json', error: 'request body too large' }],
    });

    const firstFile = new File(['{"type":"codex"}'], 'first-auth.json', {
      type: 'application/json',
    });
    const secondFile = new File(['{"type":"claude"}'], 'second-auth.json', {
      type: 'application/json',
    });

    // Act
    const result = await authFilesApi.uploadFiles([firstFile, secondFile]);

    // Assert
    expect(result).toEqual({
      status: 'partial',
      uploaded: 1,
      files: ['first-auth.json'],
      failed: [{ name: 'second-auth.json', error: 'request body too large' }],
    });
    expect(mocks.postForm).toHaveBeenCalledTimes(1);
  });

  it('saveJsonObject throws Upload failed when backend reports zero uploaded files without explicit failures', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 0,
      files: [],
      failed: [],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('failed-converted-auth.json', {
        type: 'codex',
        access_token: 'token',
      })
    ).rejects.toThrow('Upload failed');
  });

  it('saveText throws Upload failed when backend reports zero uploaded files without explicit failures', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 0,
      files: [],
      failed: [],
    });

    // Act / Assert
    await expect(authFilesApi.saveText('failed-auth.json', '{"type":"codex"}')).rejects.toThrow(
      'Upload failed'
    );
  });

  it('saveJsonObject surfaces backend failure error text', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'partial',
      uploaded: 0,
      files: [],
      failed: [{ name: 'converted-auth.json', error: 'Storage quota exceeded' }],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('converted-auth.json', {
        type: 'codex',
        access_token: 'token',
      })
    ).rejects.toThrow('Storage quota exceeded');
  });

  it('saveJsonObject throws when backend reports partial failure despite uploaded files', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'partial',
      uploaded: 1,
      files: ['converted-auth.json'],
      failed: [{ name: 'secondary-auth.json', error: 'Invalid auth payload' }],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('converted-auth.json', {
        type: 'codex',
        access_token: 'token',
      })
    ).rejects.toThrow('Invalid auth payload');
  });

  it('saveJsonObject throws when backend reports explicit error status without upload counters', async () => {
    // Arrange
    mocks.postForm.mockResolvedValue({
      status: 'error',
      files: [],
      failed: [],
    });

    // Act / Assert
    await expect(
      authFilesApi.saveJsonObject('failed-status-auth.json', {
        type: 'codex',
        access_token: 'token',
      })
    ).rejects.toThrow('Upload failed');
  });
});

describe('authFilesApi patchFieldsForAuthIndexes', () => {
  const getUploadedFile = () => {
    const formData = mocks.postForm.mock.calls[0]?.[1];
    expect(formData).toBeInstanceOf(FormData);
    const file = (formData as FormData).get('file');
    expect(file).toBeInstanceOf(File);
    return file as File;
  };

  it('updates only matching auth records in an auth array', async () => {
    mocks.getRaw.mockResolvedValue({
      data: new Blob([
        JSON.stringify([
          { type: 'codex', authIndex: 0, priority: 1, websocket: true },
          { type: 'codex', auth_index: 'auth-2', priority: 2 },
          { type: 'codex', authIndex: 'auth-3', priority: 3, websocket: true },
        ]),
      ]),
    });
    mocks.postForm.mockResolvedValue({
      status: 'ok',
      uploaded: 1,
      files: ['shared-codex.json'],
      failed: [],
    });

    await authFilesApi.patchFieldsForAuthIndexes('shared-codex.json', [0, 'auth-2'], {
      priority: 10,
      websockets: false,
    });

    expect(mocks.getRaw).toHaveBeenCalledWith('/auth-files/download?name=shared-codex.json', {
      responseType: 'blob',
    });
    expect(mocks.postForm).toHaveBeenCalledWith('/auth-files', expect.any(FormData));
    const file = getUploadedFile();
    expect(file.name).toBe('shared-codex.json');
    await expect(file.text()).resolves.toBe(
      JSON.stringify([
        { type: 'codex', authIndex: 0, priority: 10, websockets: false },
        { type: 'codex', auth_index: 'auth-2', priority: 10, websockets: false },
        { type: 'codex', authIndex: 'auth-3', priority: 3, websocket: true },
      ])
    );
  });
});

describe('authFilesApi batch management contracts', () => {
  it('updates many statuses with one PATCH request', async () => {
    mocks.patch.mockResolvedValueOnce({
      status: 'partial',
      updated: 1,
      items: [{ name: 'alpha.json', auth_index: 'auth-a', disabled: true }],
      failed: [{ name: 'missing.json', error: 'auth file not found' }],
    });

    const result = await authFilesApi.setStatuses(
      [{ name: 'alpha.json', authIndex: 'auth-a' }, { name: 'missing.json' }],
      true
    );

    expect(mocks.patch).toHaveBeenCalledWith('/auth-files/status/batch', {
      items: [{ name: 'alpha.json', auth_index: 'auth-a' }, { name: 'missing.json' }],
      disabled: true,
    });
    expect(result).toEqual({
      status: 'partial',
      updated: 1,
      items: [{ name: 'alpha.json', authIndex: 'auth-a', disabled: true }],
      failed: [{ name: 'missing.json', error: 'auth file not found' }],
    });
  });

  it('patches fields for grouped auth indexes with one request', async () => {
    mocks.patch.mockResolvedValueOnce({ status: 'ok', updated: 2, failed: [] });

    const result = await authFilesApi.patchFieldsBatch(
      [{ name: 'shared.json', authIndexes: ['auth-1', 'auth-2'] }],
      { priority: 8 }
    );

    expect(mocks.patch).toHaveBeenCalledWith('/auth-files/fields/batch', {
      items: [{ name: 'shared.json', auth_indices: ['auth-1', 'auth-2'] }],
      fields: { priority: 8 },
    });
    expect(result).toEqual({ status: 'ok', updated: 2, failed: [] });
  });

  it('downloads selected files as one ZIP response', async () => {
    const blob = new Blob(['zip'], { type: 'application/zip' });
    mocks.requestRaw.mockResolvedValueOnce({
      data: blob,
      headers: {
        'content-disposition': 'attachment; filename="auth-files-20260715.zip"',
        'x-auth-files-included': '2',
        'x-auth-files-failed': '1',
      },
    });

    const result = await authFilesApi.downloadFiles(['alpha.json', 'beta.json', 'missing.json']);

    expect(mocks.requestRaw).toHaveBeenCalledWith({
      method: 'POST',
      url: '/auth-files/download',
      data: { names: ['alpha.json', 'beta.json', 'missing.json'] },
      responseType: 'blob',
    });
    expect(result).toEqual({
      blob,
      filename: 'auth-files-20260715.zip',
      included: 2,
      failed: 1,
    });
  });
});
