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
  it('keeps same-domain fixed provider credentials separate by auth index', () => {
    const resources = buildProviderResources(
      [
        {
          type: 'codex',
          configs: [
            { apiKey: 'sk-example-key-01', baseUrl: 'https://example.com/v1', authIndex: 'auth-1' },
            {
              apiKey: 'sk-example-key-02',
              baseUrl: 'https://example.com/other',
              authIndex: 'auth-2',
            },
            {
              name: 'Secondary route',
              apiKey: 'sk-second-key',
              baseUrl: 'https://second.example.com/v1',
              authIndex: 'auth-3',
            },
          ],
        },
      ],
      [],
      labels
    );

    expect(resources).toEqual([
      {
        key: 'codex\u0000auth-1',
        label: 'Codex · example.com · sk******01',
        authIndices: ['auth-1'],
      },
      {
        key: 'codex\u0000auth-2',
        label: 'Codex · example.com · sk******02',
        authIndices: ['auth-2'],
      },
      {
        key: 'codex\u0000auth-3',
        label: 'Codex · Secondary route',
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
