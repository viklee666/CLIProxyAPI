import type { CredentialBindingSelection } from '@/services/api';

export type CredentialQuerySelectionState = {
  all: boolean;
  selectCurrentPlan: boolean;
  planFilter: string;
  providers: string[];
  excludedAuthIndices: string[];
};

const normalizedValues = (values: string[]) =>
  Array.from(
    new Set(
      values.map((value) => value.trim().toLowerCase()).filter((value) => value && value !== 'all')
    )
  ).sort();

const normalizedExactValues = (values: string[]) =>
  Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort();

export const isQueryCredentialSelectionActive = (state: CredentialQuerySelectionState) =>
  state.all || state.selectCurrentPlan || normalizedValues(state.providers).length > 0;

export const buildCredentialBindingSelection = (
  state: CredentialQuerySelectionState
): CredentialBindingSelection | null => {
  if (!isQueryCredentialSelectionActive(state)) return null;
  if (state.all) {
    return {
      mode: 'query',
      all: true,
      providers: [],
      plan_types: [],
      excluded_auth_indices: normalizedExactValues(state.excludedAuthIndices),
    };
  }
  const providers = normalizedValues(state.providers);
  const planFilter = state.planFilter.trim().toLowerCase();
  const planTypes =
    planFilter && planFilter !== 'all' && (state.selectCurrentPlan || providers.length > 0)
      ? [planFilter]
      : [];
  return {
    mode: 'query',
    all: false,
    providers,
    plan_types: planTypes,
    excluded_auth_indices: normalizedExactValues(state.excludedAuthIndices),
  };
};
