import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApiCallRequest } from './apiCall';

const mocks = vi.hoisted(() => ({
  post: vi.fn(),
}));

vi.mock('./client', () => ({ apiClient: mocks }));

import { modelsApi } from './models';

const expectedModels = [{ name: 'claude-sonnet-test', alias: 'Claude Sonnet Test' }];
const modelsResponse = {
  status_code: 200,
  header: {},
  body: JSON.stringify({
    data: [
      { id: 'claude-sonnet-test', display_name: 'Claude Sonnet Test' },
      { id: 'claude-sonnet-test' },
    ],
  }),
};
const invalidKeyResponse = {
  status_code: 401,
  header: {},
  body: JSON.stringify({ error: { message: 'Invalid API key' } }),
};

beforeEach(() => {
  mocks.post.mockReset().mockResolvedValue(modelsResponse);
});

describe('Claude model discovery authentication', () => {
  it('uses the same Bearer authentication as Codex and OpenAI for a third-party models endpoint', async () => {
    mocks.post.mockImplementation(async (_path: string, request: ApiCallRequest) =>
      request.header?.Authorization === 'Bearer upstream-key' ? modelsResponse : invalidKeyResponse
    );

    const baseUrl = 'https://gateway.example/v1';
    for (const fetchModels of [
      modelsApi.fetchV1ModelsViaApiCall,
      modelsApi.fetchModelsViaApiCall,
      modelsApi.fetchClaudeModelsViaApiCall,
    ]) {
      await expect(fetchModels(baseUrl, 'upstream-key')).resolves.toEqual(expectedModels);
    }

    expect(mocks.post).toHaveBeenCalledTimes(3);
    for (const [path, request] of mocks.post.mock.calls) {
      expect(path).toBe('/api-call');
      expect(request).toMatchObject({
        method: 'GET',
        url: 'https://gateway.example/v1/models',
        header: { Authorization: 'Bearer upstream-key' },
      });
      expect(request.header).not.toHaveProperty('x-api-key');
    }
  });

  it.each(['', 'https://api.anthropic.com', 'https://API.Anthropic.com:443/v1/'])(
    'keeps x-api-key authentication for the official Anthropic endpoint %s',
    async (baseUrl) => {
      await expect(
        modelsApi.fetchClaudeModelsViaApiCall(baseUrl, ' official-key ')
      ).resolves.toEqual(expectedModels);

      expect(mocks.post).toHaveBeenCalledWith(
        '/api-call',
        {
          authIndex: undefined,
          method: 'GET',
          url: modelsApi.buildClaudeModelsEndpoint(baseUrl),
          header: {
            'x-api-key': 'official-key',
            'anthropic-version': '2023-06-01',
          },
        },
        undefined
      );
    }
  );

  it.each([
    'https://gateway.example',
    'http://api.anthropic.com',
    'https://api.anthropic.com:8443',
    'https://api.anthropic.com.gateway.example',
    'https://user:password@api.anthropic.com',
  ])('uses Bearer outside the official Anthropic origin: %s', async (baseUrl) => {
    await modelsApi.fetchClaudeModelsViaApiCall(baseUrl, ' gateway-key ');

    expect(mocks.post).toHaveBeenCalledWith(
      '/api-call',
      {
        authIndex: undefined,
        method: 'GET',
        url: modelsApi.buildClaudeModelsEndpoint(baseUrl),
        header: {
          Authorization: 'Bearer gateway-key',
          'anthropic-version': '2023-06-01',
        },
      },
      undefined
    );
  });

  it.each([
    { baseUrl: 'https://api.anthropic.com', authentication: { 'x-api-key': '$TOKEN$' } },
    { baseUrl: 'https://gateway.example/v1', authentication: { Authorization: 'Bearer $TOKEN$' } },
  ])(
    'uses the saved credential with the correct header at $baseUrl',
    async ({ baseUrl, authentication }) => {
      await modelsApi.fetchClaudeModelsViaApiCall(baseUrl, undefined, {}, ' saved-auth ');

      expect(mocks.post).toHaveBeenCalledWith(
        '/api-call',
        {
          authIndex: 'saved-auth',
          method: 'GET',
          url: modelsApi.buildClaudeModelsEndpoint(baseUrl),
          header: { ...authentication, 'anthropic-version': '2023-06-01' },
        },
        undefined
      );
    }
  );

  it.each<{ name: string; headers: Record<string, string> }>([
    { name: 'Authorization', headers: { aUtHoRiZaTiOn: 'Bearer custom-key' } },
    { name: 'x-api-key', headers: { 'X-API-Key': 'custom-key' } },
    {
      name: 'both authentication headers',
      headers: { Authorization: 'Bearer custom-bearer', 'x-api-key': 'custom-api-key' },
    },
    { name: 'a saved Bearer token', headers: { authorization: 'Bearer $TOKEN$' } },
  ])('preserves custom $name without injecting a competing credential', async ({ headers }) => {
    for (const baseUrl of ['https://api.anthropic.com', 'https://gateway.example']) {
      const customHeaders = { ...headers, 'X-Trace-ID': 'request-id' };
      await modelsApi.fetchClaudeModelsViaApiCall(
        baseUrl,
        'field-key',
        customHeaders,
        'saved-auth'
      );

      expect(mocks.post).toHaveBeenLastCalledWith(
        '/api-call',
        {
          authIndex: 'saved-auth',
          method: 'GET',
          url: modelsApi.buildClaudeModelsEndpoint(baseUrl),
          header: { ...customHeaders, 'anthropic-version': '2023-06-01' },
        },
        undefined
      );
      expect(customHeaders).toEqual({ ...headers, 'X-Trace-ID': 'request-id' });
    }
  });

  it('keeps a case-insensitive custom Anthropic version header', async () => {
    await modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key', {
      'Anthropic-Version': 'custom-version',
    });

    expect(mocks.post.mock.calls[0][1].header).toEqual({
      'Anthropic-Version': 'custom-version',
      Authorization: 'Bearer gateway-key',
    });
  });

  it.each(['https://api.anthropic.com', 'https://gateway.example'])(
    'does not invent a credential when fetching without a key at %s',
    async (baseUrl) => {
      await expect(modelsApi.fetchClaudeModelsViaApiCall(baseUrl)).resolves.toEqual(expectedModels);
      expect(mocks.post.mock.calls[0][1].header).toEqual({
        'anthropic-version': '2023-06-01',
      });
    }
  );

  it('reports a real upstream 401 without an anonymous retry and allows a later refresh', async () => {
    mocks.post.mockResolvedValueOnce(invalidKeyResponse);

    await expect(
      modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key')
    ).rejects.toThrow('401 Invalid API key');
    expect(mocks.post).toHaveBeenCalledTimes(1);

    await expect(
      modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key')
    ).resolves.toEqual(expectedModels);
    expect(mocks.post).toHaveBeenCalledTimes(2);
  });

  it('shares identical in-flight requests and clears them after completion', async () => {
    let completeRequest!: (response: typeof modelsResponse) => void;
    mocks.post.mockImplementationOnce(
      () =>
        new Promise<typeof modelsResponse>((resolve) => {
          completeRequest = resolve;
        })
    );

    const first = modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key');
    const second = modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key');
    expect(mocks.post).toHaveBeenCalledTimes(1);

    completeRequest(modelsResponse);
    await expect(Promise.all([first, second])).resolves.toEqual([expectedModels, expectedModels]);

    await modelsApi.fetchClaudeModelsViaApiCall('https://gateway.example', 'gateway-key');
    expect(mocks.post).toHaveBeenCalledTimes(2);
  });
});
