import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('./client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    put: mocks.put,
    patch: mocks.patch,
    delete: mocks.delete,
  },
}));

import {
  normalizePluginDeleteResult,
  normalizePluginList,
  normalizePluginStoreList,
  normalizePluginStoreInstallResult,
  pluginsApi,
  pluginStoreApi,
} from './plugins';

beforeEach(() => {
  mocks.get.mockReset();
  mocks.post.mockReset();
  mocks.put.mockReset();
  mocks.patch.mockReset();
  mocks.delete.mockReset();
});

describe('plugin API normalizers', () => {
  it('normalizes plugin list responses and filters invalid entries', () => {
    const result = normalizePluginList({
      plugins_enabled: true,
      plugins_dir: 'custom-plugins',
      plugins: [
        {
          id: 'demo',
          path: '/plugins/demo',
          configured: true,
          registered: true,
          enabled: false,
          effective_enabled: true,
          supports_oauth: true,
          logo: '/plugins/demo/logo.png',
          config_fields: [
            {
              name: 'mode',
              type: 'enum',
              enum_values: ['fast', 'safe'],
              description: 'Mode',
            },
          ],
          menus: [{ path: '/plugins/demo/page', menu: 'Demo', description: 'Demo page' }],
          metadata: {
            name: 'Demo Plugin',
            version: '1.0.0',
            author: 'CPA',
            github_repository: 'owner/repo',
          },
        },
        { path: '/missing-id' },
      ],
    });

    expect(result.pluginsEnabled).toBe(true);
    expect(result.pluginsDir).toBe('custom-plugins');
    expect(result.plugins).toHaveLength(1);
    expect(result.plugins[0]).toMatchObject({
      id: 'demo',
      oauthProvider: 'demo',
      enabled: false,
      effectiveEnabled: true,
      supportsOAuth: true,
      metadata: {
        name: 'Demo Plugin',
        githubRepository: 'owner/repo',
      },
    });
    expect(result.plugins[0]?.configFields[0]?.enumValues).toEqual(['fast', 'safe']);
    expect(result.plugins[0]?.menus[0]?.path).toBe('/plugins/demo/page');
    expect(result).toMatchObject({
      page: 1,
      pageSize: 1,
      total: 1,
      totalPages: 1,
      hasMore: false,
    });
  });

  it('normalizes explicit plugin OAuth providers without legacy fallback override', () => {
    const result = normalizePluginList({
      plugins: [
        {
          id: 'legacy-plugin',
          supports_oauth: true,
          oauth_provider: 'custom-oauth-provider',
        },
        {
          id: 'no-provider',
          supports_oauth: true,
          oauth_provider: '',
        },
      ],
    });

    expect(result.plugins[0]).toMatchObject({
      id: 'legacy-plugin',
      oauthProvider: 'custom-oauth-provider',
    });
    expect(result.plugins[1]).toMatchObject({
      id: 'no-provider',
      supportsOAuth: true,
    });
    expect(result.plugins[1]?.oauthProvider).toBeUndefined();
  });

  it('normalizes plugin delete results', () => {
    expect(
      normalizePluginDeleteResult({
        status: 'deleted',
        id: 'demo',
        path: '/plugins/demo.so',
        file_deleted: true,
        configured_removed: true,
        restart_required: false,
      })
    ).toEqual({
      status: 'deleted',
      id: 'demo',
      path: '/plugins/demo.so',
      fileDeleted: true,
      configuredRemoved: true,
      restartRequired: false,
    });

    expect(
      normalizePluginDeleteResult({
        fileDeleted: true,
        configuredRemoved: false,
        restartRequired: true,
      })
    ).toMatchObject({
      fileDeleted: true,
      configuredRemoved: false,
      restartRequired: true,
    });
  });

  it('normalizes plugin store responses and install results', () => {
    const store = normalizePluginStoreList({
      pluginsEnabled: true,
      pluginsDir: 'plugins',
      sources: [
        { id: 'official', name: 'Official', url: 'https://example.test/registry.json' },
        { name: '', url: '' },
      ],
      source_errors: [
        {
          source_id: 'community',
          source_name: 'Community',
          source_url: 'https://community.test/registry.json',
          message: 'timeout',
        },
      ],
      page: 2,
      page_size: 25,
      total: 60,
      total_pages: 3,
      has_more: true,
      plugins: [
        {
          store_id: 'official/demo',
          source_id: 'official',
          source_name: 'Official',
          source_url: 'https://example.test/registry.json',
          id: 'demo',
          name: 'Demo',
          install_type: 'github-release',
          auth_required: true,
          auth_configured: false,
          platforms: [{ goos: 'linux', goarch: 'amd64' }],
          installed: true,
          installedVersion: '1.0.0',
          effectiveEnabled: true,
          updateAvailable: true,
          tags: ['tool', null, ''],
        },
      ],
    });

    expect(store.pluginsEnabled).toBe(true);
    expect(store.sources).toEqual([
      { id: 'official', name: 'Official', url: 'https://example.test/registry.json' },
    ]);
    expect(store.sourceErrors).toEqual([
      {
        sourceId: 'community',
        sourceName: 'Community',
        sourceUrl: 'https://community.test/registry.json',
        message: 'timeout',
      },
    ]);
    expect(store.plugins[0]).toMatchObject({
      storeId: 'official/demo',
      sourceId: 'official',
      sourceName: 'Official',
      sourceUrl: 'https://example.test/registry.json',
      id: 'demo',
      installed: true,
      installedVersion: '1.0.0',
      effectiveEnabled: true,
      updateAvailable: true,
      tags: ['tool'],
      installType: 'github-release',
      authRequired: true,
      authConfigured: false,
      platforms: [{ goos: 'linux', goarch: 'amd64' }],
    });
    expect(store).toMatchObject({
      page: 2,
      pageSize: 25,
      total: 60,
      totalPages: 3,
      hasMore: true,
    });

    expect(
      normalizePluginStoreInstallResult({
        status: 'installed',
        source_id: 'official',
        source_name: 'Official',
        source_url: 'https://example.test/registry.json',
        id: 'demo',
        version: '1.1.0',
        install_type: 'github-release',
        path: '/plugins/demo',
        plugins_enabled: true,
        restart_required: true,
      })
    ).toEqual({
      status: 'installed',
      sourceId: 'official',
      sourceName: 'Official',
      sourceUrl: 'https://example.test/registry.json',
      id: 'demo',
      version: '1.1.0',
      installType: 'github-release',
      path: '/plugins/demo',
      pluginsEnabled: true,
      restartRequired: true,
    });
  });

  it('passes the selected source when installing from the plugin store', async () => {
    mocks.post.mockResolvedValue({
      status: 'installed',
      source_id: 'official',
      id: 'demo/plugin',
    });

    const result = await pluginStoreApi.install('demo/plugin', {
      sourceId: ' official ',
      version: ' v1.2.3 ',
    });

    expect(mocks.post).toHaveBeenCalledWith(
      '/plugin-store/demo%2Fplugin/install',
      { version: 'v1.2.3' },
      {
        params: { source: 'official', version: 'v1.2.3' },
      }
    );
    expect(result).toMatchObject({
      status: 'installed',
      sourceId: 'official',
      id: 'demo/plugin',
    });
  });

  it('automatically follows plugin pages and keeps legacy id polling compatible', async () => {
    mocks.get
      .mockResolvedValueOnce({
        plugins: [{ id: 'alpha' }],
        page: 1,
        page_size: 1,
        total: 2,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        plugins: [{ id: 'bravo' }],
        page: 2,
        page_size: 1,
        total: 2,
        total_pages: 2,
        has_more: false,
      })
      .mockResolvedValueOnce({ plugins: [{ id: 'target' }] });

    const all = await pluginsApi.list();
    const target = await pluginsApi.list({ id: ' target ' });

    expect(all.plugins.map((plugin) => plugin.id)).toEqual(['alpha', 'bravo']);
    expect(all).toMatchObject({ page: 1, pageSize: 1, total: 2, totalPages: 2, hasMore: false });
    expect(target.plugins.map((plugin) => plugin.id)).toEqual(['target']);
    expect(mocks.get).toHaveBeenNthCalledWith(1, '/plugins', {
      params: { page: 1, page_size: 200 },
    });
    expect(mocks.get).toHaveBeenNthCalledWith(2, '/plugins', {
      params: { page: 2, page_size: 1 },
    });
    expect(mocks.get).toHaveBeenNthCalledWith(3, '/plugins', {
      params: { id: 'target', page: 1, page_size: 200 },
    });
  });

  it('automatically follows store pages and merges repeated source metadata', async () => {
    mocks.get
      .mockResolvedValueOnce({
        sources: [{ id: 'official', name: 'Official', url: 'https://official.test' }],
        source_errors: [
          { source_id: 'slow', source_url: 'https://slow.test', message: 'timeout' },
        ],
        plugins: [{ id: 'demo', store_id: 'official/demo-v1', source_id: 'official' }],
        page: 1,
        page_size: 1,
        total: 2,
        total_pages: 2,
        has_more: true,
      })
      .mockResolvedValueOnce({
        sources: [
          { id: 'official', name: 'Official', url: 'https://official.test' },
          { id: 'community', name: 'Community', url: 'https://community.test' },
        ],
        source_errors: [
          { source_id: 'slow', source_url: 'https://slow.test', message: 'timeout' },
        ],
        plugins: [{ id: 'demo', store_id: 'official/demo-v2', source_id: 'official' }],
        page: 2,
        page_size: 1,
        total: 2,
        total_pages: 2,
        has_more: false,
      });

    const result = await pluginStoreApi.list({ id: ' demo ', sourceId: ' official ' });

    expect(result.plugins.map((plugin) => plugin.storeId)).toEqual([
      'official/demo-v1',
      'official/demo-v2',
    ]);
    expect(result.sources.map((source) => source.id)).toEqual(['official', 'community']);
    expect(result.sourceErrors).toHaveLength(1);
    expect(result.hasMore).toBe(false);
    expect(mocks.get).toHaveBeenNthCalledWith(1, '/plugin-store', {
      params: { id: 'demo', source: 'official', page: 1, page_size: 200 },
    });
    expect(mocks.get).toHaveBeenNthCalledWith(2, '/plugin-store', {
      params: { id: 'demo', source: 'official', page: 2, page_size: 1 },
    });
  });
});
