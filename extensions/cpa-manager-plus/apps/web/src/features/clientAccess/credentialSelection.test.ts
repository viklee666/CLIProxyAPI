import { describe, expect, it } from 'vitest';
import {
  buildCredentialBindingSelection,
  isQueryCredentialSelectionActive,
} from './credentialSelection';

describe('credentialSelection', () => {
  it('builds an all-credentials selector and ignores provider and plan filters', () => {
    expect(
      buildCredentialBindingSelection({
        all: true,
        selectCurrentPlan: true,
        planFilter: 'team',
        providers: ['Codex'],
        excludedAuthIndices: [' auth-b ', 'auth-b'],
      })
    ).toEqual({
      mode: 'query',
      all: true,
      providers: [],
      plan_types: [],
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
        excludedAuthIndices: [],
      })
    ).toEqual({
      mode: 'query',
      all: false,
      providers: ['claude', 'codex'],
      plan_types: ['team'],
      excluded_auth_indices: [],
    });
  });

  it('uses provider selection as a cross-page query and returns null for explicit mode', () => {
    const providerState = {
      all: false,
      selectCurrentPlan: false,
      planFilter: 'all',
      providers: ['gemini'],
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
        excludedAuthIndices: [],
      })
    ).toBeNull();
  });
});
