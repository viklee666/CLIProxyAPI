import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildProviderRows } from '../ProviderTable/rowData';
import { buildCredentialPelicanTestTargets, buildPelicanTestTargets, runPelicanTest } from './pelicanTest';

const { postV8 } = vi.hoisted(() => ({ postV8: vi.fn() }));
vi.mock('@/services/api/client', () => ({ apiClient: { postV8 } }));
beforeEach(() => postV8.mockReset());

const rows = buildProviderRows({
  gemini: [], claude: [], codex: [], vertex: [], usageByProvider: new Map(),
  openai: [{ name: 'provider name is not a file', baseUrl: 'https://provider.example/v1', authIndex: 'group-index', prefix: 'team',
    models: [{ name: 'upstream', alias: 'public-model' }],
    apiKeyEntries: [{ apiKey: 'secret-a', authIndex: 'key-a' }, { apiKey: 'secret-b' }],
  }],
});

describe('isolated Pelican targets', () => {
  it('uses routed aliases and never substitutes a provider group index for a missing key index', async () => {
    const targets = buildPelicanTestTargets(rows);
    expect(targets[0].modelNames).toEqual(['team/public-model']);
    expect(targets[0].authIndex).toBe('key-a');
    expect(targets[0].name).toBeUndefined();
    await expect(runPelicanTest(targets[1], 'model', 'low')).rejects.toThrow('runtime auth index');
    expect(postV8).not.toHaveBeenCalled();
  });

  it('sends only the exact identity and generation settings, never keys or endpoints', async () => {
    postV8.mockResolvedValue({ auth_index: 'key-a', answer: '<svg/>', model: 'actual', duration_ms: 0 });
    const signal = new AbortController().signal;
    const result = await runPelicanTest(buildPelicanTestTargets(rows)[0], 'team/public-model', 'high', 'Draw a pelican.', signal);
    expect(postV8).toHaveBeenCalledWith('/v8/management/requests/pelican', {
      auth_index: 'key-a', model: 'team/public-model', effort: 'high', prompt: 'Draw a pelican.',
    }, { timeout: 0, signal });
    expect(result).toMatchObject({ answer: '<svg/>', model: 'actual', durationMs: 0, status: 'success' });
  });

  it('retains file identity and auth index together for same-name virtual credentials', async () => {
    const targets = buildCredentialPelicanTestTargets([
      { name: 'shared.json', authIndex: 12 }, { name: 'shared.json', auth_index: 'other' },
    ]);
    expect(targets[0].id).not.toBe(targets[1].id);
    postV8.mockResolvedValue({ auth_index: '12', name: 'shared.json', answer: 'reply', duration_ms: 1 });
    await runPelicanTest(targets[0], 'm', 'medium');
    expect(postV8.mock.calls[0][1]).toMatchObject({ auth_index: '12', name: 'shared.json' });
  });

  it('rejects a response for another credential instead of reporting a false success', async () => {
    postV8.mockResolvedValue({ auth_index: 'another-key', answer: 'reply' });
    await expect(runPelicanTest(buildPelicanTestTargets(rows)[0], 'm', 'low')).rejects.toThrow('different credential');
  });
});
