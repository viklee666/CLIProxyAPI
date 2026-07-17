import { describe, expect, it } from 'vitest';
import {
  buildCredentialBindingSelection,
  isQueryCredentialSelectionActive,
  mergeCredentialAuthIndices,
} from './credentialSelection';

describe('credentialSelection', () => {
  it('keeps provider and manually selected credentials as a union', () => {
    expect(
      mergeCredentialAuthIndices(['manual-1', 'shared'], ['provider-1', 'shared'], [' manual-2 '])
    ).toEqual(['manual-1', 'manual-2', 'provider-1', 'shared']);
  });

  it('builds an all-credentials selector and ignores provider and plan filters', () => {
    expect(
      buildCredentialBindingSelection({
        all: true,
        selectCurrentPlan: true,
        planFilter: 'team',
        providers: ['Codex'],
        includedAuthIndices: ['auth-a'],
        excludedAuthIndices: [' auth-b ', 'auth-b'],
      })
    ).toEqual({
      mode: 'query',
      all: true,
      providers: [],
      plan_types: [],
      included_auth_indices: [],
      excluded_auth_indices: ['auth-b'],
    });
  });

  it('combines providers with the selected plan and normalizes values', () => {
    expect(
      buildCredentialBindingSelection({
        all: false,
        selectCurrentPlan: true,
        planFilter: ' Team ',
        providers: ['Codex', 'CLAUDE', 'codex'],
        includedAuthIndices: [' auth-1 ', 'auth-1'],
        excludedAuthIndices: [],
      })
    ).toEqual({
      mode: 'query',
      all: false,
      providers: ['claude', 'codex'],
      plan_types: ['team'],
      included_auth_indices: ['auth-1'],
      excluded_auth_indices: [],
    });
  });

  it('uses provider selection as a cross-page query and returns null for explicit mode', () => {
    const providerState = {
      all: false,
      selectCurrentPlan: false,
      planFilter: 'all',
      providers: ['gemini'],
      includedAuthIndices: [],
      excludedAuthIndices: [],
    };
    expect(isQueryCredentialSelectionActive(providerState)).toBe(true);
    expect(buildCredentialBindingSelection(providerState)?.providers).toEqual(['gemini']);
    expect(
      buildCredentialBindingSelection({
        all: false,
        selectCurrentPlan: false,
        planFilter: 'team',
        providers: [],
        includedAuthIndices: [],
        excludedAuthIndices: [],
      })
    ).toBeNull();
  });

  it('uses exact provider auth indices as a server-side restriction', () => {
    expect(
      buildCredentialBindingSelection({
        all: false,
        selectCurrentPlan: true,
        planFilter: 'plus',
        providers: [],
        includedAuthIndices: ['auth-2', ' auth-1 ', 'auth-2'],
        excludedAuthIndices: ['auth-2'],
      })
    ).toEqual({
      mode: 'query',
      all: false,
      providers: [],
      plan_types: ['plus'],
      included_auth_indices: ['auth-1', 'auth-2'],
      excluded_auth_indices: ['auth-2'],
    });
  });
});
