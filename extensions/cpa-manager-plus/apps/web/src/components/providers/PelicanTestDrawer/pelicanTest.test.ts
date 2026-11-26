import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildProviderRows } from '../ProviderTable/rowData';
import { buildCredentialPelicanTestTargets, buildNativePelicanTestTargets, buildPelicanTestTargets, candyPrompt, pelicanBenchmarks, pelicanPrompt, resolvePelicanTestTarget, runPelicanTest } from './pelicanTest';

const { getV8, postV8 } = vi.hoisted(() => ({ getV8: vi.fn(), postV8: vi.fn() }));
vi.mock('@/services/api/client', () => ({ apiClient: { getV8, postV8 } }));
beforeEach(() => { getV8.mockReset(); postV8.mockReset(); });

const rows = buildProviderRows({
  gemini: [], claude: [], codex: [], vertex: [], usageByProvider: new Map(),
  openai: [{ name: 'provider name is not a file', baseUrl: 'https://provider.example/v1', authIndex: 'group-index', prefix: 'team',
    models: [{ name: 'upstream', alias: 'public-model' }],
    apiKeyEntries: [{ apiKey: 'secret-a', authIndex: 'key-a' }, { apiKey: 'secret-b' }],
  }],
});

describe('isolated Pelican targets', () => {
  it('uses original configuration/key positions and resolves models from the exact identity', async () => {
    const targets = buildPelicanTestTargets([{ ...rows[0], originalIndex: 5 }]);
    expect(targets.map((target) => target.selector)).toEqual([
      { provider_kind: 'openai', config_index: 5, key_index: 0 },
      { provider_kind: 'openai', config_index: 5, key_index: 1 },
    ]);
    getV8.mockResolvedValue({ auth_id: 'runtime-key-b', auth_index: 'collision', name: '', models: ['team/public-model'] });
    const signal = new AbortController().signal;
    const resolved = await resolvePelicanTestTarget(targets[1], signal);
    expect(getV8).toHaveBeenCalledWith('/v8/management/requests/pelican', { params: targets[1].selector, signal });
    expect(resolved).toMatchObject({ authId: 'runtime-key-b', modelNames: ['team/public-model'] });
    postV8.mockResolvedValue({ auth_id: 'runtime-key-b', auth_index: 'collision', answer: '<svg/>', model: 'actual', duration_ms: 0 });
    const result = await runPelicanTest(resolved, 'team/public-model', 'high', 'Draw a pelican.', signal);
    expect(postV8).toHaveBeenCalledWith('/v8/management/requests/pelican', {
      auth_id: 'runtime-key-b', model: 'team/public-model', effort: 'high', prompt: 'Draw a pelican.',
    }, { timeout: 0, signal });
    expect(result).toMatchObject({ model: 'actual', durationMs: 0, status: 'success' });
  });

  it('prefers credential IDs and preserves name/index together without falling back on ambiguity', async () => {
    const targets = buildCredentialPelicanTestTargets([
      { id: 'exact-id', name: 'shared.json', authIndex: 12 },
      { name: 'shared.json', auth_index: 'other' },
    ]);
    expect(targets.map((target) => target.selector)).toEqual([
      { auth_id: 'exact-id' }, { name: 'shared.json', auth_index: 'other' },
    ]);
    getV8.mockRejectedValue(new Error('Ambiguous credential'));
    await expect(resolvePelicanTestTarget(targets[1])).rejects.toThrow('Ambiguous');
    expect(postV8).not.toHaveBeenCalled();
  });

  it('rejects mismatched discovery and generation identities even when auth indices collide', async () => {
    const target = buildCredentialPelicanTestTargets([{ id: 'expected', name: 'shared.json' }])[0];
    getV8.mockResolvedValue({ auth_id: 'other', auth_index: 'same', name: 'shared.json', models: ['m'] });
    await expect(resolvePelicanTestTarget(target)).rejects.toThrow('different credential');
    getV8.mockResolvedValue({ auth_id: 'expected', auth_index: 'same', name: 'shared.json', models: ['m'] });
    const resolved = await resolvePelicanTestTarget(target);
    postV8.mockResolvedValue({ auth_id: 'other', auth_index: 'same', answer: 'reply' });
    await expect(runPelicanTest(resolved, 'm', 'low')).rejects.toThrow('different credential');
  });

  it('covers native providers with per-kind positions and no secret values', () => {
    const configs = [{ name: 'native', apiKey: 'secret', authIndex: 'collision' }, { apiKey: 'secret-2' }];
    const targets = [...buildNativePelicanTestTargets('xai', configs), ...buildNativePelicanTestTargets('meta', configs)];
    expect(targets.map((target) => target.selector)).toEqual([
      { provider_kind: 'xai', config_index: 0 }, { provider_kind: 'xai', config_index: 1 },
      { provider_kind: 'meta', config_index: 0 }, { provider_kind: 'meta', config_index: 1 },
    ]);
    expect(JSON.stringify(targets)).not.toContain('secret');
  });

  it('keeps exactly the three original challenge modes and never appends the reference answer', async () => {
    expect(Object.keys(pelicanBenchmarks)).toEqual(['animation', 'candy', 'custom']);
    expect(pelicanPrompt).toBe('创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要有任何限制');
    expect(candyPrompt).toContain('圆形 | 7 | 9 | 8');
    expect(candyPrompt).toContain('五角星 | 7 | 6 | 4');
    expect(candyPrompt).toContain('不能在取出后获知口味');
    expect(candyPrompt).not.toContain('21');
    const target = buildCredentialPelicanTestTargets([{ id: 'exact', name: 'one' }])[0];
    getV8.mockResolvedValue({ auth_id: 'exact', auth_index: '1', name: 'one', models: ['m'] });
    const resolved = await resolvePelicanTestTarget(target);
    postV8.mockResolvedValue({ auth_id: 'exact', answer: '21' });
    await runPelicanTest(resolved, 'm', 'medium', candyPrompt);
    expect(postV8.mock.calls[0][1]).toEqual({ auth_id: 'exact', model: 'm', effort: 'medium', prompt: candyPrompt });
  });
});
