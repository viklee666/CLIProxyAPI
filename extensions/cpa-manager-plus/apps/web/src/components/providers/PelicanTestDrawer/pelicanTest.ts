import { apiCallApi, getApiCallErrorMessage, modelsApi } from '@/services/api';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { OpenAIProviderConfig } from '@/types';
import {
  buildClaudeMessagesEndpoint,
  buildCodexResponsesEndpoint,
  buildOpenAIChatCompletionsEndpoint,
} from '../utils';
import type { ProviderKind, ProviderRow } from '../ProviderTable/rowData';

export type PelicanStrength = 'low' | 'medium' | 'high' | 'xhigh';
export type PelicanTestStatus = 'pending' | 'running' | 'success' | 'error';

export interface PelicanTestTarget {
  id: string;
  providerKey: string;
  providerKind: ProviderKind;
  label: string;
  subtitle: string;
  modelNames: string[];
  row: ProviderRow;
  keyIndex?: number;
}

export interface PelicanTestResult {
  id: string;
  status: PelicanTestStatus;
  model: string;
  durationMs?: number;
  answer?: string;
  message?: string;
  passed?: boolean;
}

const PELICAN_PROMPT = 'Calculate 37 + 5. Reply with only the final number.';
const STRENGTH_BUDGETS: Record<PelicanStrength, number> = {
  low: 512,
  medium: 1024,
  high: 2048,
  xhigh: 4096,
};

const isHeader = (headers: Record<string, string>, name: string) =>
  Object.keys(headers).some((key) => key.toLowerCase() === name.toLowerCase());
const getCredentials = (target: PelicanTestTarget) => {
  const config = target.row.raw;
  const openAIConfig = target.providerKind === 'openai' ? (config as OpenAIProviderConfig) : undefined;
  const entry = openAIConfig?.apiKeyEntries?.[target.keyIndex ?? 0];
  const apiKey = String((openAIConfig ? entry?.apiKey : config.apiKey) ?? '').trim();
  const authIndex = normalizeAuthIndex(
    target.providerKind === 'openai' ? entry?.authIndex ?? config.authIndex : config.authIndex
  ) ?? undefined;
  const headers = {
    ...(config.headers ?? {}),
    ...(entry?.headers ?? {}),
  };
  if (!isHeader(headers, 'authorization') && !isHeader(headers, 'x-api-key') && !isHeader(headers, 'x-goog-api-key')) {
    if (apiKey) {
      headers[target.providerKind === 'claude' ? 'x-api-key' : 'Authorization'] =
        target.providerKind === 'claude' ? apiKey : `Bearer ${apiKey}`;
    } else if (authIndex) {
      headers.Authorization = 'Bearer $TOKEN$';
    }
  }
  return { headers, authIndex };
};

const responseText = (body: unknown, bodyText: string): string => {
  if (body && typeof body === 'object') {
    const value = body as Record<string, unknown>;
    if (typeof value.output_text === 'string') return value.output_text;
    const choice = Array.isArray(value.choices) ? value.choices[0] : undefined;
    if (choice && typeof choice === 'object') {
      const message = (choice as Record<string, unknown>).message;
      if (message && typeof message === 'object') {
        const content = (message as Record<string, unknown>).content;
        if (typeof content === 'string') return content;
      }
    }
    const content = Array.isArray(value.content) ? value.content[0] : undefined;
    if (content && typeof content === 'object' && typeof (content as Record<string, unknown>).text === 'string') {
      return String((content as Record<string, unknown>).text);
    }
    const candidates = Array.isArray(value.candidates) ? value.candidates[0] : undefined;
    if (candidates && typeof candidates === 'object') {
      const candidateContent = (candidates as Record<string, unknown>).content;
      if (candidateContent && typeof candidateContent === 'object') {
        const parts = (candidateContent as Record<string, unknown>).parts;
        if (Array.isArray(parts) && parts[0] && typeof parts[0] === 'object') {
          const text = (parts[0] as Record<string, unknown>).text;
          if (typeof text === 'string') return text;
        }
      }
    }
  }
  return bodyText;
};

const buildRequest = (target: PelicanTestTarget, model: string, strength: PelicanStrength) => {
  const budget = STRENGTH_BUDGETS[strength];
  const { headers, authIndex } = getCredentials(target);
  let url = '';
  let data: Record<string, unknown>;
  switch (target.providerKind) {
    case 'codex':
      url = buildCodexResponsesEndpoint(target.row.baseUrl);
      data = {
        model,
        input: [{ role: 'user', content: [{ type: 'input_text', text: PELICAN_PROMPT }] }],
        reasoning: { effort: strength },
        store: false,
        stream: false,
      };
      break;
    case 'claude':
      url = buildClaudeMessagesEndpoint(target.row.baseUrl);
      data = {
        model,
        max_tokens: Math.max(128, budget),
        messages: [{ role: 'user', content: PELICAN_PROMPT }],
        ...(strength === 'low' ? {} : { thinking: { type: 'enabled', budget_tokens: budget } }),
      };
      if (!isHeader(headers, 'anthropic-version')) headers['anthropic-version'] = '2023-06-01';
      break;
    case 'gemini':
    case 'interactions':
    case 'vertex': {
      const endpoint = modelsApi.buildGeminiModelsEndpoint(target.row.baseUrl);
      url = endpoint ? `${endpoint.replace(/\/models$/i, `/models/${encodeURIComponent(model)}`)}:generateContent` : '';
      data = {
        contents: [{ role: 'user', parts: [{ text: PELICAN_PROMPT }] }],
        generationConfig: { temperature: 0, maxOutputTokens: Math.max(128, budget) },
        ...(strength === 'low' ? {} : { thinkingConfig: { thinkingBudget: budget } }),
      };
      break;
    }
    case 'openai':
    default:
      url = buildOpenAIChatCompletionsEndpoint(target.row.baseUrl);
      data = {
        model,
        messages: [{ role: 'user', content: PELICAN_PROMPT }],
        temperature: 0,
        reasoning_effort: strength,
      };
      break;
  }
  return { url, authIndex, headers, data };
};

export const buildPelicanTestTargets = (rows: ProviderRow[]): PelicanTestTarget[] =>
  rows.flatMap((row): PelicanTestTarget[] => {
    if (row.kind !== 'openai') {
      return [{
        id: row.key,
        providerKey: row.key,
        providerKind: row.kind,
        label: row.label || row.kind,
        subtitle: row.baseUrl,
        modelNames: row.modelNames,
        row,
      }];
    }
    const entries = row.raw.apiKeyEntries ?? [];
    if (!entries.length) {
      return [{ id: `${row.key}:key:0`, providerKey: row.key, providerKind: row.kind, label: row.label || row.kind, subtitle: row.baseUrl, modelNames: row.modelNames, row, keyIndex: 0 }];
    }
    return entries.map((_, keyIndex) => ({
      id: `${row.key}:key:${keyIndex}`,
      providerKey: row.key,
      providerKind: row.kind,
      label: `${row.label || row.kind} · Key #${keyIndex + 1}`,
      subtitle: row.baseUrl,
      modelNames: row.modelNames,
      row,
      keyIndex,
    }));
  });

export const runPelicanTest = async (
  target: PelicanTestTarget,
  model: string,
  strength: PelicanStrength
): Promise<PelicanTestResult> => {
  const startedAt = Date.now();
  const request = buildRequest(target, model, strength);
  if (!request.url) throw new Error('Invalid provider endpoint');
  const result = await apiCallApi.request({
    authIndex: request.authIndex,
    method: 'POST',
    url: request.url,
    header: { ...request.headers, 'Content-Type': 'application/json' },
    data: JSON.stringify(request.data),
  });
  if (result.statusCode < 200 || result.statusCode >= 300) {
    throw new Error(getApiCallErrorMessage(result));
  }
  const answer = responseText(result.body, result.bodyText).trim();
  return {
    id: target.id,
    status: 'success',
    model,
    durationMs: Date.now() - startedAt,
    answer,
    passed: /(^|\D)42(\D|$)/.test(answer),
    message: answer ? undefined : 'Empty response',
  };
};

export const pelicanPrompt = PELICAN_PROMPT;
export const pelicanStrengths: PelicanStrength[] = ['low', 'medium', 'high', 'xhigh'];
