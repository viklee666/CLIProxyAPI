import { apiClient } from '@/services/api/client';
import type { AuthFileItem, ProviderKeyConfig } from '@/types';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { ProviderRow } from '../ProviderTable/rowData';

export type PelicanStrength = 'low' | 'medium' | 'high' | 'xhigh';
export type PelicanTestStatus = 'pending' | 'running' | 'success' | 'error' | 'cancelled';

export type PelicanTargetSelector =
  | { provider_kind: ProviderRow['kind'] | 'xai' | 'meta'; config_index: number; key_index?: number }
  | { auth_id: string }
  | { name: string; auth_index?: string };

export interface PelicanTestTarget {
  id: string;
  label: string;
  subtitle: string;
  selector: PelicanTargetSelector;
}

export interface ResolvedPelicanTestTarget extends PelicanTestTarget {
  authId: string;
  authIndex: string;
  name: string;
  modelNames: string[];
}

export interface PelicanTestResult {
  id: string;
  status: PelicanTestStatus;
  model: string;
  durationMs?: number;
  answer?: string;
  message?: string;
  usage?: unknown;
}

// Cockpit Tools d4b7de6697efc4bc251962e56d71ff6ffa05e956; fixed wording across locales.
export const pelicanPrompt = '创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要有任何限制';
export const candyPrompt = `不要搜索资料，自己思考：

袋子里有三种口味的糖——苹果、桃子、西瓜。每种口味又有两种形状：圆形和五角星。摸糖的时候，形状靠手感就能分辨，但不能分辨口味。

不同口味和形状的数量如下：
形状 | 苹果 | 桃子 | 西瓜
圆形 | 7 | 9 | 8
五角星 | 7 | 6 | 4

问题是：最少取多少颗，能保证手里同时有一组「圆形苹果 + 五角星桃子」或者「五角星苹果 + 圆形桃子」？

允许按形状选择拿取，每颗取出后不放回，不能根据口味挑选，也不能在取出后获知口味。请给出最少数量、保证成功的取法，以及更少数量不能保证成功的理由。用纯文本回答，不要生成 HTML。`;
export const pelicanBenchmarks = { animation: pelicanPrompt, candy: candyPrompt, custom: '' } as const;
export type PelicanBenchmark = keyof typeof pelicanBenchmarks;
export const pelicanBenchmark = (prompt: string): PelicanBenchmark =>
  prompt === pelicanPrompt ? 'animation' : prompt === candyPrompt ? 'candy' : 'custom';
export const pelicanStrengths: PelicanStrength[] = ['low', 'medium', 'high', 'xhigh'];

const indexValue = (value: unknown): string | undefined => normalizeAuthIndex(value) ?? undefined;

export const buildCredentialPelicanTestTargets = (files: AuthFileItem[]): PelicanTestTarget[] =>
  files.map((file, index) => {
    const authIndex = indexValue(file.authIndex ?? file.auth_index);
    const authId = typeof file.id === 'string' ? file.id.trim() : '';
    return {
      id: `credential:${authId || authIndex || file.name}:${index}`,
      label: file.name,
      subtitle: String(file.provider || file.type || ''),
      selector: authId ? { auth_id: authId } : { name: file.name, ...(authIndex ? { auth_index: authIndex } : {}) },
    };
  });

export const buildPelicanTestTargets = (rows: ProviderRow[]): PelicanTestTarget[] =>
  rows.flatMap((row) => {
    const base = { label: row.label || row.kind, subtitle: row.baseUrl };
    const selector = { provider_kind: row.kind, config_index: row.originalIndex };
    if (row.kind !== 'openai' || !row.raw.apiKeyEntries?.length) {
      return [{ ...base, id: row.key, selector }];
    }
    return row.raw.apiKeyEntries.map((_, keyIndex) => ({
      ...base,
      id: `${row.key}:key:${keyIndex}`,
      label: `${base.label} · Key #${keyIndex + 1}`,
      selector: { ...selector, key_index: keyIndex },
    }));
  });

export const buildNativePelicanTestTargets = (kind: 'xai' | 'meta', configs: ProviderKeyConfig[]): PelicanTestTarget[] =>
  configs.map((config, configIndex) => ({
    id: `${kind}:${configIndex}`,
    label: `${kind === 'xai' ? 'xAI' : 'Meta'} · ${config.name || `#${configIndex + 1}`}`,
    subtitle: config.baseUrl || '',
    selector: { provider_kind: kind, config_index: configIndex },
  }));

type PelicanIdentityResponse = { auth_id: string; auth_index: string; name: string; models: string[] };
export const resolvePelicanTestTarget = async (target: PelicanTestTarget, signal?: AbortSignal): Promise<ResolvedPelicanTestTarget> => {
  const identity = await apiClient.getV8<PelicanIdentityResponse>('/v8/management/requests/pelican', {
    params: target.selector, signal,
  });
  if (!identity.auth_id || !Array.isArray(identity.models)) throw new Error('Target has no exact runtime identity');
  if ('auth_id' in target.selector && target.selector.auth_id !== identity.auth_id) {
    throw new Error('Model list belongs to a different credential');
  }
  return { ...target, authId: identity.auth_id, authIndex: identity.auth_index, name: identity.name, modelNames: identity.models };
};

type PelicanResponse = {
  auth_id: string;
  auth_index: string;
  name: string;
  model: string;
  answer: string;
  duration_ms: number;
  usage?: unknown;
};

export const runPelicanTest = async (
  target: ResolvedPelicanTestTarget,
  model: string,
  strength: PelicanStrength,
  prompt = pelicanPrompt,
  signal?: AbortSignal
): Promise<PelicanTestResult> => {
  if (!target.authId) throw new Error('Target has no exact runtime identity');
  const response = await apiClient.postV8<PelicanResponse>('/v8/management/requests/pelican', {
    auth_id: target.authId,
    model,
    prompt,
    effort: strength,
  }, { timeout: 0, signal });
  if (response.auth_id !== target.authId) {
    throw new Error('Test response belongs to a different credential');
  }
  if (!response.answer?.trim()) throw new Error('Test generation returned no text answer');
  return {
    id: target.id,
    status: 'success',
    model: response.model || model,
    durationMs: response.duration_ms,
    answer: response.answer,
    usage: response.usage,
  };
};
