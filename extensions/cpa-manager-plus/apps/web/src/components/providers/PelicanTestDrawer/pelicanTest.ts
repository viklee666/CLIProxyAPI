import { apiClient } from '@/services/api/client';
import type { AuthFileItem } from '@/types';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { ProviderRow } from '../ProviderTable/rowData';

export type PelicanStrength = 'low' | 'medium' | 'high' | 'xhigh';
export type PelicanTestStatus = 'pending' | 'running' | 'success' | 'error' | 'cancelled';

export interface PelicanTestTarget {
  id: string;
  label: string;
  subtitle: string;
  modelNames: string[];
  authIndex?: string;
  name?: string;
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

// Keep the challenge identical across targets; it is a visual comparison, not an IQ score.
export const pelicanPrompt = 'Create a self-contained HTML document with an inline SVG 2D animation of a pelican riding a bicycle. Use inline SVG and CSS only, with no scripts or external resources. Return only the complete HTML document.';
export const pelicanStrengths: PelicanStrength[] = ['low', 'medium', 'high', 'xhigh'];

const indexValue = (value: unknown): string | undefined => normalizeAuthIndex(value) ?? undefined;

export const buildCredentialPelicanTestTargets = (files: AuthFileItem[]): PelicanTestTarget[] =>
  files.map((file) => {
    const authIndex = indexValue(file.authIndex ?? file.auth_index);
    return {
      id: `credential:${authIndex ?? file.name}:${file.name}`,
      label: file.name,
      subtitle: String(file.provider || file.type || ''),
      modelNames: [],
      authIndex,
      name: file.name,
    };
  });

export const buildPelicanTestTargets = (rows: ProviderRow[]): PelicanTestTarget[] =>
  rows.flatMap((row) => {
    const prefix = row.raw.prefix?.trim().replace(/\/+$/, '');
    const modelNames = Array.from(new Set((row.raw.models ?? []).map((model) => {
      const name = model.alias?.trim() || model.name.trim();
      return prefix ? `${prefix}/${name}` : name;
    }).filter(Boolean)));
    const base = { label: row.label || row.kind, subtitle: row.baseUrl, modelNames };
    if (row.kind !== 'openai') {
      return [{ ...base, id: row.key, authIndex: indexValue(row.raw.authIndex) }];
    }
    const entries = row.raw.apiKeyEntries ?? [];
    if (!entries.length) {
      return [{ ...base, id: row.key, authIndex: indexValue(row.raw.authIndex) }];
    }
    return entries.map((entry, keyIndex) => ({
      ...base,
      id: `${row.key}:key:${keyIndex}`,
      label: `${base.label} · Key #${keyIndex + 1}`,
      // A missing key index must never fall back to another key in the group.
      authIndex: indexValue(entry.authIndex),
    }));
  });

type PelicanResponse = {
  auth_index: string;
  name: string;
  model: string;
  answer: string;
  duration_ms: number;
  usage?: unknown;
};

export const runPelicanTest = async (
  target: PelicanTestTarget,
  model: string,
  strength: PelicanStrength,
  prompt = pelicanPrompt,
  signal?: AbortSignal
): Promise<PelicanTestResult> => {
  if (!target.authIndex && !target.name) throw new Error('Target has no runtime auth index');
  const response = await apiClient.postV8<PelicanResponse>('/v8/management/requests/pelican', {
    ...(target.authIndex ? { auth_index: target.authIndex } : {}),
    ...(target.name ? { name: target.name } : {}),
    model,
    prompt,
    effort: strength,
  }, { timeout: 0, signal });
  if (target.authIndex && response.auth_index !== target.authIndex) {
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
