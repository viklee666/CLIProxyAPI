export interface RequestScopedErrorRule {
  status?: number;
  match?: string[];
  matchRegexr?: string[];
  action?: string;
}

export type ParsedRequestScopedErrors =
  | { rules: RequestScopedErrorRule[]; error: '' }
  | { rules: null; error: string };

export const formatRequestScopedErrors = (rules?: RequestScopedErrorRule[]) =>
  JSON.stringify(rules ?? [], null, 2);

export const parseRequestScopedErrors = (text: string): ParsedRequestScopedErrors => {
  let value: unknown;
  try {
    value = JSON.parse(text || '[]');
  } catch {
    return { rules: null, error: 'invalid_json' };
  }
  if (!Array.isArray(value)) return { rules: null, error: 'expected_array' };

  const rules: RequestScopedErrorRule[] = [];
  for (const item of value) {
    if (item === null || typeof item !== 'object' || Array.isArray(item)) {
      return { rules: null, error: 'expected_rule' };
    }
    const record = item as Record<string, unknown>;
    const status = record.status;
    if (status !== undefined && (!Number.isInteger(status) || typeof status !== 'number')) {
      return { rules: null, error: 'invalid_status' };
    }
    const match = record.match;
    const matchRegexr = record['match-regexr'] ?? record.matchRegexr ?? record.match_regexr;
    if (
      (match !== undefined &&
        (!Array.isArray(match) || match.some((entry) => typeof entry !== 'string'))) ||
      (matchRegexr !== undefined &&
        (!Array.isArray(matchRegexr) || matchRegexr.some((entry) => typeof entry !== 'string')))
    ) {
      return { rules: null, error: 'invalid_match' };
    }
    const action = record.action;
    if (action !== undefined && typeof action !== 'string') {
      return { rules: null, error: 'invalid_action' };
    }
    rules.push({
      ...(status === undefined ? {} : { status: status as number }),
      ...(match === undefined ? {} : { match: match as string[] }),
      ...(matchRegexr === undefined ? {} : { matchRegexr: matchRegexr as string[] }),
      ...(action === undefined ? {} : { action }),
    });
  }
  return { rules, error: '' };
};
