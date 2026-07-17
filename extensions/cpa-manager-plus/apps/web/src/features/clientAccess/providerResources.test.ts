import { describe, expect, it } from 'vitest';
import { buildProviderResources, type ProviderLabels } from './providerResources';

const labels: ProviderLabels = {
  defaultEndpoint: 'Default endpoint',
  gemini: 'Gemini',
  interactions: 'Interactions',
  codex: 'Codex',
  claude: 'Claude',
  vertex: 'Vertex',
  openAICompatible: 'OpenAI Compatible',
};

describe('buildProviderResources', () => {
  it('groups fixed providers by type and upstream domain', () => {
    const resources = buildProviderResources(
      [
        {
          type: 'codex',
          configs: [
            { baseUrl: 'https://example.com/v1', authIndex: 'auth-1' },
            { baseUrl: 'https://example.com/other', authIndex: 'auth-2' },
            { baseUrl: 'https://second.example.com/v1', authIndex: 'auth-3' },
          ],
        },
      ],
      [],
      labels
    );

    expect(resources).toEqual([
      {
        key: 'codex\u0000example.com',
        label: 'Codex · example.com',
        authIndices: ['auth-1', 'auth-2'],
      },
      {
        key: 'codex\u0000second.example.com',
        label: 'Codex · second.example.com',
        authIndices: ['auth-3'],
      },
    ]);
  });

  it('groups OpenAI-compatible providers by configured name', () => {
    const resources = buildProviderResources(
      [],
      [
        {
          name: 'Primary upstream',
          apiKeyEntries: [{ authIndex: 'auth-1' }, { authIndex: 'auth-2' }],
        },
      ],
      labels
    );

    expect(resources).toEqual([
      {
        key: 'openai-compatibility\u0000primary upstream',
        label: 'OpenAI Compatible · Primary upstream',
        authIndices: ['auth-1', 'auth-2'],
      },
    ]);
  });
});
