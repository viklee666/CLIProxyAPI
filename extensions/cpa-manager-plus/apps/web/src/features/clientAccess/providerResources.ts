export type ProviderResource = { key: string; label: string; authIndices: string[] };

export type ProviderLabels = {
  defaultEndpoint: string;
  gemini: string;
  interactions: string;
  codex: string;
  claude: string;
  vertex: string;
  openAICompatible: string;
};

type ProviderConfigWithAuthIndex = { baseUrl?: string; authIndex?: string };

const uniqueAuthIndices = (values: Array<string | null | undefined>) =>
  Array.from(new Set(values.map((value) => String(value ?? '').trim()).filter(Boolean)));

const providerDomain = (baseUrl: string | undefined, defaultEndpoint: string) => {
  const value = String(baseUrl ?? '').trim();
  if (!value) return defaultEndpoint;
  try {
    return new URL(value).host || value;
  } catch {
    return value.replace(/^https?:\/\//i, '').split('/')[0] || value;
  }
};

export const buildProviderResources = (
  fixedProviders: Array<{
    type: keyof Omit<ProviderLabels, 'defaultEndpoint' | 'openAICompatible'>;
    configs: ProviderConfigWithAuthIndex[];
  }>,
  openAIProviders: Array<{
    name: string;
    authIndex?: string;
    apiKeyEntries?: Array<{ authIndex?: string }>;
  }>,
  labels: ProviderLabels
) => {
  const resources = new Map<string, ProviderResource>();
  const add = (key: string, label: string, authIndices: Array<string | null | undefined>) => {
    const indices = uniqueAuthIndices(authIndices);
    if (indices.length === 0) return;
    const current = resources.get(key);
    if (current) {
      current.authIndices = uniqueAuthIndices([...current.authIndices, ...indices]);
      return;
    }
    resources.set(key, { key, label, authIndices: indices });
  };

  for (const provider of fixedProviders) {
    for (const config of provider.configs) {
      const domain = providerDomain(config.baseUrl, labels.defaultEndpoint);
      add(`${provider.type}\u0000${domain.toLowerCase()}`, `${labels[provider.type]} · ${domain}`, [
        config.authIndex,
      ]);
    }
  }
  for (const provider of openAIProviders) {
    const name = String(provider.name ?? '').trim();
    if (!name) continue;
    add(`openai-compatibility\u0000${name.toLowerCase()}`, `${labels.openAICompatible} · ${name}`, [
      provider.authIndex,
      ...(provider.apiKeyEntries ?? []).map((entry) => entry.authIndex),
    ]);
  }
  return Array.from(resources.values()).sort((left, right) =>
    left.label.localeCompare(right.label)
  );
};
